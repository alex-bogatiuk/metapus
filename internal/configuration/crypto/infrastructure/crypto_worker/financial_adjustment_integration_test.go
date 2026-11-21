//go:build integration

package crypto_worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo/receipttest"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestFinancialAdjustmentRepeatedPartialReversal(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		paymentID, custodyID, merchantID, feeID := attributedRemediationPayment(t, ctx, f, id.New(), "partial")
		service := cryptoapp.NewFinancialAdjustmentService(postgres.NewContextTxManager(), crypto_repo.NewFinancialAdjustmentRepo())
		maker, checker := id.New(), id.New()
		propose := func(key, custody, merchant, fee string) *cryptoapp.FinancialAdjustment {
			adjustment, err := service.Propose(ctx, cryptoapp.ProposeFinancialAdjustmentCommand{
				Mode: "payment_reversal", PaymentID: paymentID, PostedVersion: 1,
				SourceLines: []cryptoapp.FinancialAdjustmentSourceSelection{
					{Register: "suspense", LineID: custodyID, Amount: custody},
					{Register: "merchant", LineID: merchantID, Amount: merchant},
					{Register: "fee", LineID: feeID, Amount: fee},
				},
				ReasonCode: "incorrect_payment_posting", Evidence: json.RawMessage(`{"description":"incorrect settlement effect"}`),
				MakerID: maker, IdempotencyKey: key,
			})
			require.NoError(t, err)
			return adjustment
		}
		first := propose("partial-first-"+paymentID.String(), "30", "28", "2")
		_, err := service.Approve(ctx, first.ID, checker, "checked partial movements")
		require.NoError(t, err)
		sources, err := service.PaymentSources(ctx, paymentID)
		require.NoError(t, err)
		require.Equal(t, "70", sources.Lines[0].Available)
		second := propose("partial-second-"+paymentID.String(), "70", "67", "3")
		_, err = service.Approve(ctx, second.ID, checker, "checked remaining movements")
		require.NoError(t, err)
		var total string
		require.NoError(t, q.QueryRow(ctx, `SELECT SUM(amount)::text FROM reg_crypto_receipt_suspense_movements
			WHERE reversal_of_line_id=$1`, custodyID).Scan(&total))
		require.Equal(t, "100", total)
		_, err = service.Propose(ctx, cryptoapp.ProposeFinancialAdjustmentCommand{
			Mode: "payment_reversal", PaymentID: paymentID,
			SourceLines: []cryptoapp.FinancialAdjustmentSourceSelection{
				{Register: "suspense", LineID: custodyID, Amount: "1"},
				{Register: "merchant", LineID: merchantID, Amount: "1"},
			}, ReasonCode: "payment_other", Evidence: json.RawMessage(`{"description":"another correction attempt"}`),
			MakerID: maker, IdempotencyKey: "partial-over-" + paymentID.String(),
		})
		require.Error(t, err)
	})
}

func TestFinancialAdjustmentPartialReversalBeforeReorg(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		invoiceID := id.New()
		paymentID, custodyID, merchantID, feeID := attributedRemediationPayment(t, ctx, f, invoiceID, "reorg")
		var txHash string
		require.NoError(t, q.QueryRow(ctx, `SELECT tx_hash FROM doc_crypto_payments WHERE id=$1`, paymentID).Scan(&txHash))
		service := cryptoapp.NewFinancialAdjustmentService(postgres.NewContextTxManager(), crypto_repo.NewFinancialAdjustmentRepo())
		adjustment, err := service.Propose(ctx, cryptoapp.ProposeFinancialAdjustmentCommand{
			Mode: "payment_reversal", PaymentID: paymentID,
			SourceLines: []cryptoapp.FinancialAdjustmentSourceSelection{
				{Register: "suspense", LineID: custodyID, Amount: "30"},
				{Register: "merchant", LineID: merchantID, Amount: "28"},
				{Register: "fee", LineID: feeID, Amount: "2"},
			}, ReasonCode: "incorrect_payment_posting",
			Evidence: json.RawMessage(`{"description":"partial correction before reorg"}`),
			MakerID:  id.New(), IdempotencyKey: "adjustment-reorg-" + paymentID.String(), Direct: true,
		})
		require.NoError(t, err)
		require.Equal(t, "posted", adjustment.Status)
		payment := &crypto_payment.CryptoPayment{
			NetworkID: f.network, InvoiceID: invoiceID, WalletID: f.wallet,
			TokenID: f.token, MerchantID: f.merchant, TxHash: txHash,
			EventKind: "native_value", Amount: types.NewRawAmountFromInt64(100),
		}
		payment.ID, payment.PostedVersion = paymentID, 1
		payment.FeeFixed = types.NewRawAmountFromInt64(5)
		require.NoError(t, q.QueryRow(ctx, `SELECT receipt_id,event_kind FROM doc_crypto_payments WHERE id=$1`, paymentID).Scan(&payment.ReceiptID, &payment.EventKind))
		reorg := crypto_repo.NewFinancialReorgRepo()
		require.NoError(t, reorg.RecordFinancialReorg(ctx, payment, crypto.BlockchainEvent{BlockNumber: 1, BlockHash: "orphaned"}))
		for _, item := range []struct {
			table     string
			source    id.ID
			remainder string
		}{
			{"reg_crypto_receipt_suspense_movements", custodyID, "70"},
			{"reg_crypto_merchant_balance_movements", merchantID, "67"},
			{"reg_crypto_fee_movements", feeID, "3"},
		} {
			var amount string
			require.NoError(t, q.QueryRow(ctx, `SELECT amount::text FROM `+item.table+
				` WHERE recorder_type='CryptoPaymentReorg' AND reversal_of_line_id=$1`, item.source).Scan(&amount))
			require.Equal(t, item.remainder, amount)
		}
		var custody, unidentified string
		require.NoError(t, q.QueryRow(ctx, `SELECT (SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2),
			(SELECT amount::text FROM reg_crypto_receipt_suspense_balances WHERE receipt_id=$3)`, f.wallet, f.token, payment.ReceiptID).Scan(&custody, &unidentified))
		require.Equal(t, []string{"100", "100"}, []string{custody, unidentified}, "the receipt keeps custody; the attribution returns to suspense")
	})
}

// attributedRemediationPayment records a 100-unit receipt and a settled
// payment attributing it as 95 to the merchant and a fixed fee of 5. It
// returns the payment and its suspense, merchant and fee lines.
func attributedRemediationPayment(t *testing.T, ctx context.Context, f remediationFixture, invoiceID id.ID, label string) (payment, suspense, merchant, fee id.ID) {
	t.Helper()
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	sum := sha256.Sum256([]byte(label + f.code))
	txHash := hex.EncodeToString(sum[:])
	receipt, proof := receipttest.Record(t, ctx, receipttest.Deps{Proofs: crypto_repo.NewBlockIngestionRepo(), Repo: crypto_repo.NewReceiptIntakeRepo(), Guard: crypto_repo.NewBlockIngestionRepo()},
		receipttest.Transfer{WalletID: f.wallet, TokenID: f.token, TxHash: txHash, Amount: "100", BlockNumber: 100})
	payment, suspense, merchant, fee = id.New(), id.New(), id.New(), id.New()
	_, err := q.Exec(ctx, `INSERT INTO doc_crypto_payments
		(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,event_kind,amount,status,posted,posted_version,fee_fixed,receipt_id,verification_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,'native_value',100,'settled',TRUE,1,5,$8,$9)`,
		payment, invoiceID, f.merchant, f.network, f.token, f.wallet, txHash, receipt, proof)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_receipt_suspense_movements
		(line_id,recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
		VALUES($1,$2,'CryptoPayment',1,NOW(),'expense',$3,$4,100)`, suspense, payment, receipt, f.token)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements
		(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
		VALUES($1,$2,'CryptoPayment',1,NOW(),'receipt',$3,$4,95)`, merchant, payment, f.merchant, f.token)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_movements
		(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
		VALUES($1,$2,'CryptoPayment',1,NOW(),'receipt',$3,$4,'processing',5)`, fee, payment, f.merchant, f.token)
	require.NoError(t, err)
	return payment, suspense, merchant, fee
}

func TestFinancialAdjustmentWalletCorrectionDirectAndApproved(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		service := cryptoapp.NewFinancialAdjustmentService(postgres.NewContextTxManager(), crypto_repo.NewFinancialAdjustmentRepo())
		maker, checker := id.New(), id.New()
		receipt, err := service.Propose(ctx, cryptoapp.ProposeFinancialAdjustmentCommand{
			Mode: "wallet_balance", WalletID: f.wallet, TokenID: f.token, Direction: "receipt", Amount: "25", Direct: true,
			ReasonCode: "wallet_receipt", Evidence: json.RawMessage(`{"description":"manual ledger correction for test"}`),
			MakerID: maker, IdempotencyKey: "wallet-receipt-" + f.wallet.String(),
		})
		require.NoError(t, err)
		require.Equal(t, "posted", receipt.Status)
		require.Equal(t, "direct", receipt.DecisionMode)
		require.Equal(t, maker, *receipt.CheckerID)
		expense, err := service.Propose(ctx, cryptoapp.ProposeFinancialAdjustmentCommand{
			Mode: "wallet_balance", WalletID: f.wallet, TokenID: f.token, Direction: "expense", Amount: "10",
			ReasonCode: "wallet_expense", Evidence: json.RawMessage(`{"description":"manual expense correction for test"}`),
			MakerID: maker, IdempotencyKey: "wallet-expense-" + f.wallet.String(),
		})
		require.NoError(t, err)
		require.Equal(t, "pending_approval", expense.Status)
		_, err = service.Approve(ctx, expense.ID, maker, "self approval")
		require.Error(t, err)
		posted, err := service.Approve(ctx, expense.ID, checker, "approved wallet expense")
		require.NoError(t, err)
		require.Equal(t, "posted", posted.Status)
		var balance, varianceCount string
		q := txm.GetQuerier(ctx)
		require.NoError(t, q.QueryRow(ctx, `SELECT amount::text FROM reg_crypto_balance_balances
			WHERE wallet_id=$1 AND token_id=$2`, f.wallet, f.token).Scan(&balance))
		require.Equal(t, "15", balance)
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*)::text FROM reg_crypto_wallet_variance_movements
			WHERE adjustment_id IN ($1,$2)`, receipt.ID, expense.ID).Scan(&varianceCount))
		require.Equal(t, "2", varianceCount)
	})
}
