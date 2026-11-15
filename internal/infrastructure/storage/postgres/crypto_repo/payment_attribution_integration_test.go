//go:build integration

package crypto_repo

import (
	"context"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_fee"
	"metapus/internal/domain/registers/crypto_merchant_balance"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"

	"github.com/stretchr/testify/require"
)

// paymentAttributionFixture is a confirming invoice payment whose final
// transfer is already accounted as a custody receipt.
type paymentAttributionFixture struct {
	receiptIntakeFixture
	payment *crypto_payment.CryptoPayment
	receipt id.ID
	posting *posting.Engine
}

func newPaymentAttributionFixture(t *testing.T) paymentAttributionFixture {
	t.Helper()
	f := paymentAttributionFixture{receiptIntakeFixture: newReceiptIntakeFixture(t, nil)}
	merchant := id.New()
	_, err := f.pool.Exec(f.ctx, "INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Payment attribution merchant')", merchant, "PA-"+id.Compact(merchant))
	require.NoError(t, err)
	e := f.proof.Event
	f.payment = crypto_payment.NewCryptoPayment(id.New(), merchant, f.request.Basis.TokenID, f.request.Basis.WalletID, e.NetworkID, e.TxHash, e.EventIndex, string(e.EventKind), e.BlockHash, e.FromAddress, e.Amount, e.BlockNumber, 19)
	f.payment.Number = "PA-" + id.Compact(f.payment.ID)
	f.payment.Date = time.Unix(1750000003, 0).UTC()
	f.payment.Status = crypto_payment.PaymentStatusConfirming
	f.payment.Confirmations, f.payment.Canonicality, f.payment.FinalityStatus = 20, "canonical", "solidified"
	f.payment.FeeFixed = types.MustCryptoAmount("3")
	require.NoError(t, crypto_document_repo.NewCryptoPaymentRepo().Create(f.ctx, f.payment))
	// The worker assigns the proof and receipt in memory immediately before
	// posting; the database receives them in the posting callback.
	f.payment.VerificationID = f.request.Basis.VerificationID
	f.receipt, err = f.service.RecordPaymentReceipt(f.ctx, f.payment, f.request.Event)
	require.NoError(t, err)
	f.payment.ReceiptID = &f.receipt
	f.posting = f.engineWith(true, true)
	return f
}

func (f paymentAttributionFixture) engineWith(merchant, fee bool) *posting.Engine {
	e := posting.NewEngine(nil)
	e.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})
	e.AddVisitor(&cryptoposting.CryptoMerchantBalanceVisitor{})
	e.AddVisitor(&cryptoposting.CryptoFeeVisitor{})
	e.AddRecorder(cryptoposting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(crypto_register_repo.NewCryptoReceiptSuspenseRepo())))
	if merchant {
		e.AddRecorder(cryptoposting.NewCryptoMerchantBalanceRecorder(crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo())))
	}
	if fee {
		e.AddRecorder(cryptoposting.NewCryptoFeeRecorder(crypto_fee.NewService(crypto_register_repo.NewCryptoFeeRepo())))
	}
	e.ValidateInTransaction(NewBlockIngestionRepo().ValidatePosting)
	return e
}

func (f paymentAttributionFixture) post(p *crypto_payment.CryptoPayment, e *posting.Engine) error {
	return e.Post(f.ctx, p, func(ctx context.Context) error {
		p.Status = crypto_payment.PaymentStatusConfirmed
		return crypto_document_repo.NewCryptoPaymentRepo().Update(ctx, p)
	})
}

// balances returns the receipt suspense remainder, the merchant liability and
// the processing fee revenue.
func (f paymentAttributionFixture) balances(t *testing.T) []string {
	t.Helper()
	var suspense, merchant, fee string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
		COALESCE((SELECT amount FROM reg_crypto_receipt_suspense_balances WHERE receipt_id=$3 AND token_id=$1),0)::text,
		COALESCE((SELECT amount FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$2 AND token_id=$1),0)::text,
		COALESCE((SELECT SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) FROM reg_crypto_fee_movements WHERE merchant_id=$2 AND token_id=$1),0)::text`,
		f.payment.TokenID, f.payment.MerchantID, f.receipt).Scan(&suspense, &merchant, &fee))
	return []string{suspense, merchant, fee}
}

func (f paymentAttributionFixture) custody(t *testing.T) string {
	t.Helper()
	var custody string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COALESCE((SELECT amount FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2),0)::text`,
		f.payment.WalletID, f.payment.TokenID).Scan(&custody))
	return custody
}

func TestPaymentAttributesItsReceiptAndFreezesBasis(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	require.NoError(t, f.post(f.payment, f.posting))
	require.Equal(t, []string{"0", "20", "3"}, f.balances(t))
	require.Equal(t, "23", f.custody(t), "custody belongs to the receipt only")
	var paymentCustody int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1", f.payment.ID).Scan(&paymentCustody))
	require.Zero(t, paymentCustody)
	again, err := f.service.RecordPaymentReceipt(f.ctx, f.payment, f.request.Event)
	require.NoError(t, err)
	require.Equal(t, f.receipt, again, "a replayed effect resolves to its original receipt")
	_, err = f.pool.Exec(f.ctx, "UPDATE doc_crypto_payments SET fee_fixed=4 WHERE id=$1", f.payment.ID)
	require.ErrorContains(t, err, "frozen")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,'CryptoPayment',1,NOW(),'receipt',$2,$3,1)`, f.payment.ID, f.payment.WalletID, f.payment.TokenID)
	require.ErrorContains(t, err, "never moves custody")
}

func TestPaymentAttributionMissingComponentRollsBackAtomically(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	require.ErrorContains(t, f.post(f.payment, f.engineWith(true, false)), "exact receipt attribution")
	require.Equal(t, []string{"23", "0", "0"}, f.balances(t))
	require.Equal(t, "23", f.custody(t))
}

func TestPaymentAttributionCannotExceedReceiptRemainder(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	require.NoError(t, f.post(f.payment, f.posting))
	twin := crypto_payment.NewCryptoPayment(id.New(), f.payment.MerchantID, f.payment.TokenID, f.payment.WalletID, f.payment.NetworkID,
		f.payment.TxHash, f.payment.EventIndex+1, f.payment.EventKind, f.payment.BlockHash, f.payment.FromAddress, f.payment.Amount, f.payment.BlockNumber, 19)
	twin.Number, twin.Date, twin.Status = "PA-"+id.Compact(twin.ID), f.payment.Date, crypto_payment.PaymentStatusConfirming
	twin.Canonicality, twin.FinalityStatus = "canonical", "solidified"
	require.NoError(t, crypto_document_repo.NewCryptoPaymentRepo().Create(f.ctx, twin))
	twin.VerificationID, twin.ReceiptID = f.payment.VerificationID, &f.receipt
	require.Error(t, f.post(twin, f.posting), "one receipt cannot be attributed twice")
	require.Equal(t, []string{"0", "20", "3"}, f.balances(t))
}

func TestPaymentReorgReturnsAttributionAndReinclusionReusesReceipt(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	require.NoError(t, f.post(f.payment, f.posting))
	event := f.request.Event
	event.BlockNumber++
	require.NoError(t, NewFinancialReorgRepo().RecordFinancialReorg(f.ctx, f.payment, event))
	require.Equal(t, []string{"23", "0", "0"}, f.balances(t), "the merchant credit is reversed automatically")
	require.Equal(t, "23", f.custody(t), "the receipt stays under its own canonicality audit")
	var holds int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_network_holds WHERE network_id=$1 AND reason='posted_payment_reorg' AND status='open'`,
		f.payment.NetworkID).Scan(&holds))
	require.Equal(t, 1, holds)

	f.payment.MarkUnposted()
	f.payment.Status = crypto_payment.PaymentStatusReorged
	require.NoError(t, crypto_document_repo.NewCryptoPaymentRepo().Update(f.ctx, f.payment))
	receipt, err := f.service.RecordPaymentReceipt(f.ctx, f.payment, f.request.Event)
	require.NoError(t, err)
	require.Equal(t, f.receipt, receipt)
	require.NoError(t, f.post(f.payment, f.posting))
	require.Equal(t, 2, f.payment.PostedVersion)
	require.Equal(t, []string{"0", "20", "3"}, f.balances(t))
	require.Equal(t, "23", f.custody(t))
}

func TestOrphanedReceiptCannotBeAttributed(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	incident := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,incident_kind,tx_hash,details)
		VALUES($1,$2,'deep_reorg',$3,'{}')`, incident, f.payment.NetworkID, f.payment.TxHash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_custody_receipt_audits SET status='orphaned',orphaned_at=NOW(),incident_id=$2 WHERE receipt_id=$1`, f.receipt, incident)
	require.NoError(t, err)
	require.ErrorContains(t, f.post(f.payment, f.posting), "orphaned receipt")
	require.Equal(t, []string{"23", "0", "0"}, f.balances(t))
}
