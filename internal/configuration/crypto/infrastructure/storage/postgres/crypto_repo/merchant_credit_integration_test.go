//go:build integration

package crypto_repo

import (
	"context"
	"sync"
	"testing"
	"time"

	"metapus/internal/application/command"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_merchant_credit"
	"metapus/internal/configuration/crypto/domain/registers/crypto_fee"
	"metapus/internal/configuration/crypto/domain/registers/crypto_merchant_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_receipt_suspense"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/stretchr/testify/require"
)

type fixedTariff struct{}

func (fixedTariff) Resolve(context.Context, id.ID, id.ID, crypto.FeeDirection) (crypto.EffectiveFee, error) {
	return crypto.EffectiveFee{FixedFee: types.MustRawAmount("1"), PercentBP: 100,
		MinFee: types.ZeroRawAmount(), MaxFee: types.ZeroRawAmount()}, nil
}

type merchantCreditFixture struct {
	receiptIntakeFixture
	credits  *cryptoapp.MerchantCreditService
	receipt  id.ID
	merchant id.ID
}

func newMerchantCreditFixture(t *testing.T, custodyOwner bool) merchantCreditFixture {
	t.Helper()
	f := newReceiptIntakeFixture(t, nil)
	merchant := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Credit merchant')`, merchant, "MC-"+id.Compact(merchant))
	require.NoError(t, err)
	if custodyOwner {
		_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET merchant_id=$2,custody_merchant_id=$2 WHERE id=$1`, f.request.Basis.WalletID, merchant)
		require.NoError(t, err)
	}
	doc, err := f.service.Record(f.ctx, f.request)
	require.NoError(t, err)
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoMerchantBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoFeeVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(crypto_register_repo.NewCryptoReceiptSuspenseRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoMerchantBalanceRecorder(crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoFeeRecorder(crypto_fee.NewService(crypto_register_repo.NewCryptoFeeRepo())))
	credits := cryptoapp.NewMerchantCreditService(postgres.MustGetTxManager(f.ctx), NewMerchantCreditRepo(), fixedTariff{},
		engine, numerator.New(), postgres.NewOutboxPublisher(), time.Now)
	return merchantCreditFixture{receiptIntakeFixture: f, credits: credits, receipt: doc.ID, merchant: merchant}
}

func (f merchantCreditFixture) amounts(t *testing.T) (suspense, merchant, fee string) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
		(SELECT amount::text FROM reg_crypto_receipt_suspense_balances WHERE receipt_id=$1),
		COALESCE((SELECT SUM(amount)::text FROM reg_crypto_merchant_balance_movements WHERE merchant_id=$2),'0'),
		COALESCE((SELECT SUM(amount)::text FROM reg_crypto_fee_movements WHERE merchant_id=$2),'0')`, f.receipt, f.merchant).
		Scan(&suspense, &merchant, &fee))
	return
}

func TestMerchantCreditBelowThresholdPostsAtOnce(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_receipt_credit_policies(token_id,dual_approval_threshold) VALUES($1,1000)`, f.request.Basis.TokenID)
	require.NoError(t, err)
	actor := id.New()
	doc, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: actor})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusPosted, doc.Status)
	require.False(t, doc.ApprovalRequired)
	gross := f.request.Basis.Amount
	fee, net := crypto.EffectiveFee{FixedFee: types.MustRawAmount("1"), PercentBP: 100}.Calculate(gross), types.ZeroRawAmount()
	net = gross.Sub(fee)
	suspense, merchant, charged := f.amounts(t)
	require.Equal(t, []string{"0", net.String(), fee.String()}, []string{suspense, merchant, charged})
	var custody int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1`, doc.ID).Scan(&custody))
	require.Zero(t, custody, "attribution never moves custody")
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: actor})
	require.ErrorContains(t, err, "no unattributed remainder")
}

func TestMerchantCreditAboveThresholdNeedsAnotherOperator(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	preparer := id.New()
	doc, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: preparer})
	require.NoError(t, err, "without a policy every credit needs a second operator")
	require.Equal(t, crypto_merchant_credit.StatusPrepared, doc.Status)
	suspense, merchant, _ := f.amounts(t)
	require.Equal(t, []string{f.request.Basis.Amount.String(), "0"}, []string{suspense, merchant})

	_, err = f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, ExpectedVersion: doc.Version, Actor: preparer})
	require.True(t, apperror.IsForbidden(err), "%v", err)
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.Error(t, err, "one open credit per receipt")

	approved, err := f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, ExpectedVersion: doc.Version, Actor: id.New()})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusPosted, approved.Status)
	suspense, merchant, _ = f.amounts(t)
	require.Equal(t, "0", suspense)
	require.Equal(t, approved.NetAmount.String(), merchant)

	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_merchant_credits SET amount=amount+1 WHERE id=$1`, doc.ID)
	require.ErrorContains(t, err, "single decision")
}

func TestMerchantCreditRejectionKeepsSuspenseAndAllowsNewCredit(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	doc, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
	_, err = f.credits.Reject(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, Actor: id.New()})
	require.Error(t, err, "rejection needs a reason")
	rejected, err := f.credits.Reject(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, Actor: id.New(), Reason: "sender is not the merchant's customer"})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusRejected, rejected.Status)
	suspense, merchant, _ := f.amounts(t)
	require.Equal(t, []string{f.request.Basis.Amount.String(), "0"}, []string{suspense, merchant})
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
}

func TestMerchantCreditRespectsCustodyOwner(t *testing.T) {
	f := newMerchantCreditFixture(t, true)
	other := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Other merchant')`, other, "MO-"+id.Compact(other))
	require.NoError(t, err)
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: other, Actor: id.New()})
	require.True(t, apperror.IsForbidden(err), "%v", err)
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
}

func TestMerchantCreditConcurrentPreparationCreditsOnce(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_receipt_credit_policies(token_id,dual_approval_threshold) VALUES($1,$2)`,
		f.request.Basis.TokenID, f.request.Basis.Amount)
	require.NoError(t, err)
	const workers = 6
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
		}()
	}
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded)
	suspense, _, _ := f.amounts(t)
	require.Equal(t, "0", suspense)
}

func TestSuspenseCannotBeConsumedWithoutItsCredit(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_receipt_suspense_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
		VALUES($1,'CryptoMerchantCredit',1,NOW(),'expense',$2,$3,1)`, id.New(), f.receipt, f.request.Basis.TokenID)
	require.ErrorContains(t, err, "exact merchant credit")
}

func TestOrphanedReceiptCannotBeCreditedButOpenCreditCanBeRejected(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	open, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
	basis := f.request.Basis
	require.NoError(t, NewReceiptAuditRepo().MarkOrphaned(f.ctx, ReceiptAuditTask{
		ReceiptID: f.receipt, NetworkID: basis.NetworkID, WalletID: basis.WalletID, TokenID: basis.TokenID, Amount: basis.Amount,
		TxHash: basis.TxHash, EventKind: string(basis.EventKind), EventIndex: basis.EventIndex,
		BlockNumber: basis.BlockNumber, BlockHash: basis.BlockHash,
	}, *basis.VerificationID))
	// Replay of the same proven orphaning is a no-op.
	require.NoError(t, NewReceiptAuditRepo().MarkOrphaned(f.ctx, ReceiptAuditTask{ReceiptID: f.receipt, NetworkID: basis.NetworkID,
		WalletID: basis.WalletID, TokenID: basis.TokenID, Amount: basis.Amount, TxHash: basis.TxHash,
		EventKind: string(basis.EventKind), EventIndex: basis.EventIndex}, *basis.VerificationID))

	_, err = f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: open.ID, Actor: id.New()})
	require.ErrorContains(t, err, "orphaned")
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_merchant_credits SET status='posted',posted=TRUE,posted_version=1,decided_by=$2,decided_at=NOW(),version=version+1 WHERE id=$1`, open.ID, id.New())
	require.ErrorContains(t, err, "orphaned receipt cannot be attributed")
	_, err = f.credits.Reject(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: open.ID, Actor: id.New(), Reason: "transfer was orphaned"})
	require.NoError(t, err)
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.ErrorContains(t, err, "orphaned")
	suspense, merchant, _ := f.amounts(t)
	require.Equal(t, []string{basis.Amount.String(), "0"}, []string{suspense, merchant})
}

func (f merchantCreditFixture) journal(t *testing.T, state string, includeDust bool) cryptoapp.UnidentifiedReceiptList {
	t.Helper()
	list, err := receiptJournal().List(f.ctx, state, includeDust, 50, 0)
	require.NoError(t, err)
	return list
}

func TestUnidentifiedReceiptJournalFollowsTheCreditLifecycle(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	open := f.journal(t, cryptoapp.ReceiptJournalOpen, false)
	require.Equal(t, 1, open.Total)
	item := open.Items[0]
	require.Equal(t, f.receipt.String(), *item.ReceiptID)
	require.Equal(t, []string{"unattributed", f.request.Basis.Amount.String(), f.request.Basis.Amount.String(), "no_allocation"},
		[]string{item.State, item.Amount, item.Remainder, item.Reason})
	require.Equal(t, f.proof.Event.FromAddress, item.FromAddress)
	require.Nil(t, item.CreditID)

	preparer := id.New()
	credit, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: preparer})
	require.NoError(t, err)
	item = f.journal(t, cryptoapp.ReceiptJournalOpen, false).Items[0]
	require.Equal(t, "awaiting_approval", item.State)
	require.Equal(t, credit.ID.String(), *item.CreditID)
	require.Equal(t, preparer.String(), *item.CreditPreparedBy)
	require.Equal(t, "Credit merchant", *item.CreditMerchant)

	_, err = f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: credit.ID, Actor: id.New()})
	require.NoError(t, err)
	require.Zero(t, f.journal(t, cryptoapp.ReceiptJournalOpen, false).Total)
	credited := f.journal(t, cryptoapp.ReceiptJournalCredited, false)
	require.Equal(t, 1, credited.Total)
	require.Equal(t, []string{"credited", "0", "posted"}, []string{credited.Items[0].State, credited.Items[0].Remainder, *credited.Items[0].CreditStatus})
	require.Zero(t, f.journal(t, cryptoapp.ReceiptJournalOrphaned, false).Total)
	_, err = receiptJournal().List(f.ctx, "everything", false, 50, 0)
	require.Error(t, err)
}

func TestUnidentifiedReceiptJournalHidesDustByDefault(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	f.request.Reason = "dust"
	_, err := f.service.Record(f.ctx, f.request)
	require.NoError(t, err)
	list, err := receiptJournal().List(f.ctx, cryptoapp.ReceiptJournalOpen, false, 50, 0)
	require.NoError(t, err)
	require.Zero(t, list.Total)
	require.Empty(t, list.Items)
	list, err = receiptJournal().List(f.ctx, cryptoapp.ReceiptJournalOpen, true, 50, 0)
	require.NoError(t, err)
	require.Equal(t, 1, list.Total)
	require.Equal(t, "dust", list.Items[0].Reason)
}

func TestMerchantCreditCommandsAreIdempotent(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	commands := cryptoapp.NewMerchantCreditCommands(f.credits,
		command.NewExecutor(postgres.MustGetTxManager(f.ctx), postgres.NewCommandRepository()))
	operator := func() context.Context {
		return appctx.WithUser(f.ctx, &appctx.UserContext{UserID: id.New().String(), TenantID: "tenant-test", IsAdmin: true})
	}
	first := operator()
	_, err := commands.Prepare(first, "", f.receipt, f.merchant, nil)
	require.Error(t, err, "an idempotency key is required")
	prepared, err := commands.Prepare(first, "prepare-1", f.receipt, f.merchant, nil)
	require.NoError(t, err)
	require.False(t, prepared.Replayed)
	replay, err := commands.Prepare(first, "prepare-1", f.receipt, f.merchant, nil)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.JSONEq(t, string(prepared.Result.Payload), string(replay.Result.Payload))
	_, err = commands.Prepare(first, "prepare-1", f.receipt, id.New(), nil)
	require.Error(t, err, "the same key with another request is rejected")

	var result cryptoapp.MerchantCreditResult
	require.NoError(t, json.Unmarshal(prepared.Result.Payload, &result))
	require.Equal(t, crypto_merchant_credit.StatusPrepared, result.Status)
	_, err = commands.Approve(first, "approve-own", result.ID, result.Version)
	require.True(t, apperror.IsForbidden(err), "%v", err)
	second := operator()
	approved, err := commands.Approve(second, "approve-1", result.ID, result.Version)
	require.NoError(t, err)
	again, err := commands.Approve(second, "approve-1", result.ID, result.Version)
	require.NoError(t, err)
	require.True(t, again.Replayed)
	require.JSONEq(t, string(approved.Result.Payload), string(again.Result.Payload))
	suspense, merchant, _ := f.amounts(t)
	require.Equal(t, "0", suspense)
	require.NotEqual(t, "0", merchant)
	var credits int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE merchant_id=$1`, f.merchant).Scan(&credits))
	require.Equal(t, 1, credits)
}

// receiptJournal is the journal as the HTTP handler reads it.
func receiptJournal() *cryptoapp.ReceiptJournal {
	return cryptoapp.NewReceiptJournal(NewUnidentifiedReceiptRepo(), NewWalletFundingJournalRepo())
}

// Stand: operator2, an owner of the merchant, approved a credit to it.
func TestMerchantUserCannotApproveCreditToTheirMerchant(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	doc, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
	owner := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_merchant_users(user_id,merchant_id,role) VALUES($1,$2,1)`, owner, f.merchant)
	require.NoError(t, err)

	_, err = f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, ExpectedVersion: doc.Version, Actor: owner})
	require.True(t, apperror.IsForbidden(err), "%v", err)
	suspense, merchant, _ := f.amounts(t)
	require.Equal(t, []string{f.request.Basis.Amount.String(), "0"}, []string{suspense, merchant}, "nothing was posted")

	approved, err := f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, ExpectedVersion: doc.Version, Actor: id.New()})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusPosted, approved.Status)
}
