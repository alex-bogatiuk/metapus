//go:build integration

package crypto_repo

import (
	"testing"
	"time"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_platform_funding"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/register_repo"

	"github.com/stretchr/testify/require"
)

type platformFundingFixture struct {
	merchantCreditFixture
	fundings *cryptoapp.PlatformFundingService
}

func newPlatformFundingFixture(t *testing.T, custodyOwner bool) platformFundingFixture {
	t.Helper()
	f := newMerchantCreditFixture(t, custodyOwner)
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&posting.CryptoReceiptSuspenseVisitor{})
	engine.AddRecorder(posting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(register_repo.NewCryptoReceiptSuspenseRepo())))
	fundings := cryptoapp.NewPlatformFundingService(postgres.MustGetTxManager(f.ctx), NewPlatformFundingRepo(),
		engine, numerator.New(), postgres.NewOutboxPublisher(), time.Now)
	return platformFundingFixture{merchantCreditFixture: f, fundings: fundings}
}

func (f platformFundingFixture) prepare(t *testing.T, actor id.ID, acknowledge bool) (*crypto_platform_funding.CryptoPlatformFunding, error) {
	t.Helper()
	return f.fundings.Prepare(f.ctx, cryptoapp.PreparePlatformFundingCommand{ReceiptID: f.receipt,
		Reason: "own treasury transfer", AcknowledgeMerchantTie: acknowledge, Actor: actor})
}

func (f platformFundingFixture) approve(doc *crypto_platform_funding.CryptoPlatformFunding, actor id.ID) (*crypto_platform_funding.CryptoPlatformFunding, error) {
	return f.fundings.Approve(f.ctx, cryptoapp.DecidePlatformFundingCommand{FundingID: doc.ID, ExpectedVersion: doc.Version, Actor: actor})
}

func requireAppCode(t *testing.T, err error, code string) *apperror.AppError {
	t.Helper()
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok, "expected an application error, got %v", err)
	require.Equal(t, code, appErr.Code, appErr.Message)
	return appErr
}

func TestPlatformFundingTakesTheRemainderOnlyAfterAnotherOperatorApproves(t *testing.T) {
	f := newPlatformFundingFixture(t, false)
	preparer := id.New()
	doc, err := f.prepare(t, preparer, false)
	require.NoError(t, err)
	require.Equal(t, crypto_platform_funding.StatusPrepared, doc.Status)
	require.Nil(t, doc.MerchantID, "a wallet without a merchant tie needs no acknowledgement")
	suspense, _, _ := f.signedAmounts(t)
	require.Equal(t, f.request.Basis.Amount.String(), suspense, "a prepared funding moves nothing")

	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.ErrorContains(t, err, "awaits a decision", "a merchant credit cannot race a prepared platform funding")

	_, err = f.approve(doc, preparer)
	requireAppCode(t, err, apperror.CodeForbidden)
	posted, err := f.approve(doc, id.New())
	require.NoError(t, err)
	require.Equal(t, crypto_platform_funding.StatusPosted, posted.Status)

	suspense, merchant, fee := f.signedAmounts(t)
	require.Equal(t, []string{"0", "0", "0"}, []string{suspense, merchant, fee}, "only suspense moves")
	var custody int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1`, doc.ID).Scan(&custody))
	require.Zero(t, custody, "custody already holds the transfer")

	credited := f.journal(t, ReceiptJournalCredited, false)
	require.Equal(t, 1, credited.Total)
	require.Equal(t, "posted", *credited.Items[0].FundingStatus)
	detail, err := NewUnidentifiedReceiptRepo().Detail(f.ctx, f.receipt)
	require.NoError(t, err)
	require.Len(t, detail.Attributions, 1)
	require.Equal(t, []string{"CryptoPlatformFunding", "posted", f.request.Basis.Amount.String()},
		[]string{detail.Attributions[0].DocumentType, detail.Attributions[0].Status, detail.Attributions[0].Outstanding})
}

func TestPlatformFundingOfAMerchantTiedReceiptNeedsAcknowledgementAndAnUnchangedTie(t *testing.T) {
	f := newPlatformFundingFixture(t, true)
	open := f.journal(t, ReceiptJournalOpen, false)
	require.Equal(t, 1, open.Total)
	require.Equal(t, []string{f.merchant.String(), "Credit merchant", "custody_owner"},
		[]string{*open.Items[0].TieMerchantID, *open.Items[0].TieMerchantName, *open.Items[0].TieLink}, "the journal warns before preparation")

	_, err := f.prepare(t, id.New(), false)
	appErr := requireAppCode(t, err, apperror.CodeValidation)
	require.Equal(t, "Credit merchant", appErr.Details["merchantName"])
	require.Equal(t, "custody_owner", appErr.Details["merchantLink"])

	doc, err := f.prepare(t, id.New(), true)
	require.NoError(t, err)
	require.Equal(t, f.merchant, *doc.MerchantID)
	require.True(t, doc.MerchantLinkAcknowledged)

}

func TestPlatformFundingDecisionRefusesATieThatAppearedAfterPreparation(t *testing.T) {
	f := newPlatformFundingFixture(t, false)
	doc, err := f.prepare(t, id.New(), false)
	require.NoError(t, err)
	require.Nil(t, doc.MerchantID)

	// The wallet becomes a persistent deposit address of a merchant customer:
	// the decision must not take money whose tie the preparer did not see.
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET merchant_id=$2,allocation_mode='persistent',customer_id='customer-1',status='assigned'
		WHERE id=$1`, f.request.Basis.WalletID, f.merchant)
	require.NoError(t, err)
	_, err = f.approve(doc, id.New())
	require.ErrorContains(t, err, "merchant tie of the receipt changed")
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_platform_fundings SET status='posted',posted=TRUE,posted_version=1,decided_by=$2,decided_at=NOW(),version=version+1 WHERE id=$1`,
		doc.ID, id.New())
	require.ErrorContains(t, err, "merchant tie of the receipt changed", "the SQL guard refuses the same decision")
	suspense, _, _ := f.signedAmounts(t)
	require.Equal(t, f.request.Basis.Amount.String(), suspense)
}

func TestPlatformFundingIsReversedWithItsOrphanedReceipt(t *testing.T) {
	f := newPlatformFundingFixture(t, false)
	doc, err := f.prepare(t, id.New(), false)
	require.NoError(t, err)
	_, err = f.approve(doc, id.New())
	require.NoError(t, err)

	require.NoError(t, NewReceiptAuditRepo().MarkOrphaned(f.ctx, f.orphanTask(), *f.request.Basis.VerificationID))
	suspense, _, _ := f.signedAmounts(t)
	require.Equal(t, f.request.Basis.Amount.String(), suspense, "the funding returns to suspense")
	var reversals int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_platform_funding_reversals WHERE funding_id=$1`, doc.ID).Scan(&reversals))
	require.Equal(t, 1, reversals)

	orphaned := f.journal(t, ReceiptJournalOrphaned, false)
	require.Equal(t, 1, orphaned.Total)
	require.Equal(t, "reversed", *orphaned.Items[0].FundingStatus)

	_, err = f.prepare(t, id.New(), false)
	require.ErrorContains(t, err, "orphaned", "an orphaned receipt cannot be taken as platform money again")
}

func TestPlatformFundingSQLGuardsRefuseDirectWrites(t *testing.T) {
	f := newPlatformFundingFixture(t, true)
	basis := f.request.Basis
	_, err := f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_platform_fundings(id,number,date,basis_type,basis_id,receipt_id,wallet_id,token_id,amount,reason,
		status,prepared_by,prepared_at,suspense_line_id) VALUES($1,$2,NOW(),'CryptoCustodyReceipt',$3,$3,$4,$5,$6,'direct','prepared',$7,NOW(),$8)`,
		id.New(), "CPF-"+id.Compact(id.New())[:8], f.receipt, basis.WalletID, basis.TokenID, basis.Amount, id.New(), id.New())
	require.ErrorContains(t, err, "current merchant tie", "a merchant-tied receipt cannot be taken without recording its tie")

	doc, err := f.prepare(t, id.New(), true)
	require.NoError(t, err)
	_, err = f.approve(doc, id.New())
	require.NoError(t, err)
	var incident id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `INSERT INTO reg_crypto_security_incidents(network_id,incident_kind,tx_hash,event_kind,event_index)
		VALUES($1,'deep_reorg',$2,$3,$4) RETURNING id`, basis.NetworkID, basis.TxHash, string(basis.EventKind), basis.EventIndex).Scan(&incident))
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_platform_funding_reversals(id,funding_id,receipt_id,incident_id,orphan_verification_id,reason)
		VALUES($1,$2,$3,$4,$5,'receipt_orphaned')`, id.New(), doc.ID, f.receipt, incident, *basis.VerificationID)
	require.ErrorContains(t, err, "orphaned receipt and complete storno", "a storno needs the proven orphan")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_custody_receipt_audits SET status='orphaned',orphaned_at=NOW(),incident_id=$2,
		orphan_verification_id=$3 WHERE receipt_id=$1`, f.receipt, incident, *basis.VerificationID)
	require.ErrorContains(t, err, "unreversed platform funding", "an orphan cannot keep the platform funding")
}
