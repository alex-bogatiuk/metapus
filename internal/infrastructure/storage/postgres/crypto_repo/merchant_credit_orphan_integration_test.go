//go:build integration

package crypto_repo

import (
	"sync"
	"testing"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_merchant_credit"

	"github.com/stretchr/testify/require"
)

func (f merchantCreditFixture) orphanTask() ReceiptAuditTask {
	basis := f.request.Basis
	return ReceiptAuditTask{
		ReceiptID: f.receipt, NetworkID: basis.NetworkID, WalletID: basis.WalletID, TokenID: basis.TokenID, Amount: basis.Amount,
		TxHash: basis.TxHash, EventKind: string(basis.EventKind), EventIndex: basis.EventIndex,
		BlockNumber: basis.BlockNumber, BlockHash: basis.BlockHash,
	}
}

// signedAmounts returns the receipt remainder and the signed merchant and fee
// balances of the fixture merchant.
func (f merchantCreditFixture) signedAmounts(t *testing.T) (suspense, merchant, fee string) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
		(SELECT amount::text FROM reg_crypto_receipt_suspense_balances WHERE receipt_id=$1),
		COALESCE((SELECT SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END)::text
			FROM reg_crypto_merchant_balance_movements WHERE merchant_id=$2),'0'),
		COALESCE((SELECT SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END)::text
			FROM reg_crypto_fee_movements WHERE merchant_id=$2),'0')`, f.receipt, f.merchant).
		Scan(&suspense, &merchant, &fee))
	return
}

func (f merchantCreditFixture) postCredit(t *testing.T) *crypto_merchant_credit.CryptoMerchantCredit {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_receipt_credit_policies(token_id,dual_approval_threshold) VALUES($1,$2)`,
		f.request.Basis.TokenID, f.request.Basis.Amount)
	require.NoError(t, err)
	credit, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusPosted, credit.Status)
	return credit
}

func TestOrphanedReceiptReversesItsPostedManualCredit(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	credit := f.postCredit(t)
	gross := f.request.Basis.Amount.String()
	suspense, merchant, fee := f.signedAmounts(t)
	require.Equal(t, []string{"0", credit.NetAmount.String(), credit.ProcessingFee.String()}, []string{suspense, merchant, fee})

	require.NoError(t, NewReceiptAuditRepo().MarkOrphaned(f.ctx, f.orphanTask(), *f.request.Basis.VerificationID))
	suspense, merchant, fee = f.signedAmounts(t)
	require.Equal(t, []string{gross, "0", "0"}, []string{suspense, merchant, fee}, "the credit returns to suspense")

	var reversals int
	var reason string
	var sameIncident, stillPosted bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*),max(r.reason),bool_and(r.incident_id=a.incident_id
		AND r.orphan_verification_id=a.orphan_verification_id),bool_and(c.posted)
		FROM reg_crypto_merchant_credit_reversals r JOIN reg_crypto_custody_receipt_audits a ON a.receipt_id=r.receipt_id
		JOIN doc_crypto_merchant_credits c ON c.id=r.credit_id WHERE r.credit_id=$1`, credit.ID).
		Scan(&reversals, &reason, &sameIncident, &stillPosted))
	require.Equal(t, 1, reversals)
	require.Equal(t, "receipt_orphaned", reason)
	require.True(t, sameIncident, "the storno belongs to the orphaning incident")
	require.True(t, stillPosted, "the credit document keeps its history")

	// Replay of the proven orphaning reverses nothing twice.
	require.NoError(t, NewReceiptAuditRepo().MarkOrphaned(f.ctx, f.orphanTask(), *f.request.Basis.VerificationID))
	suspense, merchant, fee = f.signedAmounts(t)
	require.Equal(t, []string{gross, "0", "0"}, []string{suspense, merchant, fee})

	orphaned := f.journal(t, ReceiptJournalOrphaned, false)
	require.Equal(t, 1, orphaned.Total)
	require.Equal(t, []string{"orphaned", gross, "reversed"},
		[]string{orphaned.Items[0].State, orphaned.Items[0].Remainder, *orphaned.Items[0].CreditStatus})
	detail, err := NewUnidentifiedReceiptRepo().Detail(f.ctx, f.receipt)
	require.NoError(t, err)
	require.Len(t, detail.Attributions, 1)
	require.Equal(t, []string{"CryptoMerchantCredit", "reversed", "0"},
		[]string{detail.Attributions[0].DocumentType, detail.Attributions[0].Status, detail.Attributions[0].Outstanding})

	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.ErrorContains(t, err, "orphaned", "the returned remainder of an orphaned receipt is not creditable")
}

func TestMerchantCreditStornoRequiresTheOrphanAndItsCompleteSet(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	credit := f.postCredit(t)
	basis := f.request.Basis
	var incident id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `INSERT INTO reg_crypto_security_incidents(network_id,incident_kind,tx_hash,event_kind,event_index)
		VALUES($1,'deep_reorg',$2,$3,$4) RETURNING id`, basis.NetworkID, basis.TxHash, string(basis.EventKind), basis.EventIndex).Scan(&incident))

	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_custody_receipt_audits SET status='orphaned',orphaned_at=NOW(),incident_id=$2,
		orphan_verification_id=$3 WHERE receipt_id=$1`, f.receipt, incident, *basis.VerificationID)
	require.ErrorContains(t, err, "unreversed merchant credit")

	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_merchant_credit_reversals(id,credit_id,receipt_id,incident_id,orphan_verification_id,reason)
		VALUES($1,$2,$3,$4,$5,'receipt_orphaned')`, id.New(), credit.ID, f.receipt, incident, *basis.VerificationID)
	require.ErrorContains(t, err, "orphaned receipt and complete storno set", "a storno needs the proven orphan and every line")

	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_receipt_suspense_movements(recorder_id,recorder_type,recorder_version,period,
		record_type,receipt_id,token_id,amount,reversal_of_line_id,reversal_reason)
		SELECT $1,'CryptoPaymentReorg',1,NOW(),'receipt',receipt_id,token_id,amount,suspense_line_id,'posted_payment_reorg'
		FROM doc_crypto_merchant_credits WHERE id=$2`, id.New(), credit.ID)
	require.ErrorContains(t, err, "available attribution of its kind", "a payment compensation cannot return a manual credit")

	suspense, merchant, _ := f.signedAmounts(t)
	require.Equal(t, []string{"0", credit.NetAmount.String()}, []string{suspense, merchant})
}

func TestConcurrentCreditApprovalAndOrphaningLeaveNoCreditedOrphan(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	credit, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusPrepared, credit.Status)

	var wg sync.WaitGroup
	var approveErr, orphanErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, approveErr = f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: credit.ID, Actor: id.New()})
	}()
	go func() {
		defer wg.Done()
		orphanErr = NewReceiptAuditRepo().MarkOrphaned(f.ctx, f.orphanTask(), *f.request.Basis.VerificationID)
	}()
	wg.Wait()
	require.NoError(t, orphanErr)
	if approveErr != nil {
		require.ErrorContains(t, approveErr, "orphaned")
	}
	suspense, merchant, fee := f.signedAmounts(t)
	require.Equal(t, []string{f.request.Basis.Amount.String(), "0", "0"}, []string{suspense, merchant, fee},
		"an approval either loses to the orphan or is reversed by it")
	var unreversed int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM doc_crypto_merchant_credits c WHERE c.receipt_id=$1 AND c.posted
		AND NOT EXISTS(SELECT 1 FROM reg_crypto_merchant_credit_reversals r WHERE r.credit_id=c.id)`, f.receipt).Scan(&unreversed))
	require.Zero(t, unreversed)
}
