//go:build integration

package crypto_repo

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_merchant_credit"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/tariff"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

// reorgPayment compensates the posted payment the way the event processor does
// after a proven reorg: the attribution returns to the receipt and the payment
// is unposted but keeps its receipt for re-inclusion.
func (f paymentAttributionFixture) reorgPayment(t *testing.T) {
	t.Helper()
	event := f.request.Event
	event.BlockNumber++
	require.NoError(t, NewFinancialReorgRepo().RecordFinancialReorg(f.ctx, f.payment, event))
	f.payment.MarkUnposted()
	f.payment.Status = crypto_payment.PaymentStatusReorged
	require.NoError(t, document_repo.NewCryptoPaymentRepo().Update(f.ctx, f.payment))
}

func (f paymentAttributionFixture) merchantCredits() *cryptoapp.MerchantCreditService {
	return cryptoapp.NewMerchantCreditService(postgres.MustGetTxManager(f.ctx), NewMerchantCreditRepo(), fixedTariff{},
		f.posting, numerator.New(), postgres.NewOutboxPublisher(), time.Now)
}

func TestReorgedPaymentReceiptCannotBeCreditedManually(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	require.NoError(t, f.post(f.payment, f.posting))
	f.reorgPayment(t)
	require.Equal(t, []string{"23", "0", "0"}, f.balances(t))

	// The receipt is back in the journal, marked as waiting for the chain.
	open, err := receiptJournal().List(f.ctx, cryptoapp.ReceiptJournalOpen, false, 50, 0)
	require.NoError(t, err)
	require.Equal(t, 1, open.Total)
	require.Equal(t, "payment_reorged", open.Items[0].State)
	require.Equal(t, f.payment.ID.String(), *open.Items[0].PaymentID)
	require.Equal(t, string(crypto_payment.PaymentStatusReorged), *open.Items[0].PaymentStatus)

	_, err = f.merchantCredits().Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{
		ReceiptID: f.receipt, MerchantID: f.payment.MerchantID, Actor: id.New()})
	require.ErrorContains(t, err, "reversed by a chain reorganization")

	// The database refuses the credit even without the service check.
	doc, err := crypto_merchant_credit.Prepare(f.receipt, f.payment.MerchantID, f.payment.TokenID, nil, types.MustCryptoAmount("23"),
		tariff.Fee{Fixed: types.ZeroCryptoAmount(), Min: types.ZeroCryptoAmount(), Max: types.ZeroCryptoAmount()},
		true, id.New(), time.Now())
	require.NoError(t, err)
	doc.Number = "CMC-" + id.Compact(doc.ID)
	require.ErrorContains(t, NewMerchantCreditRepo().Create(f.ctx, doc), "reorged invoice payment cannot be credited manually")
	require.Equal(t, []string{"23", "0", "0"}, f.balances(t))

	// The re-included transfer attributes the same receipt through its payment.
	receipt, err := f.service.RecordPaymentReceipt(f.ctx, f.payment, f.request.Event)
	require.NoError(t, err)
	require.Equal(t, f.receipt, receipt)
	require.NoError(t, f.post(f.payment, f.posting))
	require.Equal(t, []string{"0", "20", "3"}, f.balances(t))
	credited, err := receiptJournal().List(f.ctx, cryptoapp.ReceiptJournalCredited, false, 50, 0)
	require.NoError(t, err)
	require.Zero(t, credited.Total, "a matched receipt held by its payment stays out of the journal")
}

func TestReincludedReceiptMovesItsAuditToTheNewBlock(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	require.NoError(t, f.post(f.payment, f.posting))
	f.reorgPayment(t)

	moved := f.proof
	// Same head as the original proof: the later block has one confirmation less.
	moved.Event.BlockNumber, moved.Event.BlockHash, moved.Event.Confirmations = 101, "0x"+strings.Repeat("d", 64), 19
	movedID, err := NewBlockIngestionRepo().SaveVerification(f.ctx, moved)
	require.NoError(t, err)
	event := moved.Event
	event.VerificationID = &movedID
	f.payment.BlockNumber, f.payment.BlockHash, f.payment.VerificationID = event.BlockNumber, event.BlockHash, &movedID
	receipt, err := f.service.RecordPaymentReceipt(f.ctx, f.payment, event)
	require.NoError(t, err)
	require.Equal(t, f.receipt, receipt, "the re-included transfer keeps its receipt")

	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_custody_receipt_audits SET next_check_at=NOW() WHERE receipt_id=$1`, f.receipt)
	require.NoError(t, err)
	tasks, err := NewReceiptAuditRepo().Claim(f.ctx, "anchor-test", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, moved.Event.BlockHash, tasks[0].BlockHash, "the audit proves the new block, not the orphaned one")
	require.Equal(t, int64(101), tasks[0].BlockNumber)

	detail, err := receiptJournal().Detail(f.ctx, f.receipt)
	require.NoError(t, err)
	require.False(t, detail.Proof.Original)
	require.Equal(t, movedID.String(), detail.Proof.VerificationID)

	// An anchor must prove this receipt's own transfer.
	foreign := f.proof
	foreign.Event.TxHash = "0x" + strings.Repeat("f", 64)
	identity, err := crypto.NewAccountingEffectIdentity(foreign.Event, crypto.ChainFamilyEVM, 2)
	require.NoError(t, err)
	foreign.AccountingIdentity = &identity
	foreignID, err := NewBlockIngestionRepo().SaveVerification(f.ctx, foreign)
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.AnchorAudit(f.ctx, f.receipt, foreignID), "canonical proof of its own transfer")
}

func TestReceiptDetailShowsProofAuditAndAttributions(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	require.NoError(t, f.post(f.payment, f.posting))
	detail, err := receiptJournal().Detail(f.ctx, f.receipt)
	require.NoError(t, err)
	require.Equal(t, "credited", detail.Row.State)
	require.Equal(t, "0", detail.Row.Remainder)
	require.True(t, detail.Proof.Original)
	require.Equal(t, f.proof.Event.TxHash, detail.Proof.TxHash)
	require.Len(t, detail.Proof.Sources, 2)
	require.Equal(t, "canonical", detail.Audit.Status)
	require.Len(t, detail.Attributions, 1)
	payment := detail.Attributions[0]
	require.Equal(t, []string{crypto_payment.Definition.Name, f.payment.ID.String(), "23", "20", "3", "23"},
		[]string{payment.DocumentType, payment.DocumentID, payment.Amount, payment.NetAmount, payment.ProcessingFee, payment.Outstanding})

	f.reorgPayment(t)
	detail, err = receiptJournal().Detail(f.ctx, f.receipt)
	require.NoError(t, err)
	require.Equal(t, []string{"payment_reorged", "23"}, []string{detail.Row.State, detail.Row.Remainder})
	require.Equal(t, "0", detail.Attributions[0].Outstanding, "the compensated payment no longer holds the receipt")
	require.False(t, detail.Attributions[0].Posted)

	_, err = receiptJournal().Detail(f.ctx, id.New())
	require.Error(t, err)
}
