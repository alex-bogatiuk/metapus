package crypto_repo

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/infrastructure/storage/postgres"
)

// Validate the stored posting after the full wallet prefix. Lifecycle labels
// may already be changed in memory by the caller; financial basis may not.
func lockFinancialReorgBasis(ctx context.Context, p *crypto_payment.CryptoPayment) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var isolation string
	if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return err
	}
	if isolation != "read committed" {
		return apperror.NewValidation("financial reorg requires read committed isolation")
	}
	if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoPayment", p.ID); err != nil {
		return err
	}
	if p.ReceiptID == nil {
		return apperror.NewConflict("financial reorg requires the attributed receipt")
	}
	if err := NewCustodyRecognitionRepo().LockWallets(ctx, p.WalletID); err != nil {
		return err
	}
	var valid bool
	err := q.QueryRow(ctx, `SELECT network_id=$2 AND invoice_id=$3 AND merchant_id=$4 AND wallet_id=$5 AND token_id=$6
 AND tx_hash=$7 AND event_kind=$8 AND event_index=$9 AND amount=$10 AND posted_version=$11
 AND block_number=$12 AND block_hash=$13 AND fee_fixed=$14 AND fee_percent_bp=$15 AND fee_min=$16 AND fee_max=$17
 AND receipt_id=$18 FROM doc_crypto_payments WHERE id=$1 FOR UPDATE`, p.ID, p.NetworkID, p.InvoiceID, p.MerchantID, p.WalletID, p.TokenID, p.TxHash, p.EventKind, p.EventIndex, p.Amount, p.PostedVersion, p.BlockNumber, p.BlockHash, p.FeeFixed, p.FeePercentBP, p.FeeMin, p.FeeMax, *p.ReceiptID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("financial reorg differs from the current payment posting")
	}
	fee, net := p.FeeAmount(), p.NetAmount()
	err = q.QueryRow(ctx, `SELECT
 (SELECT count(*)=1 AND COALESCE(bool_and(record_type='expense' AND receipt_id=$3 AND token_id=$4 AND amount=$5),FALSE)
 FROM reg_crypto_receipt_suspense_movements WHERE recorder_id=$1 AND recorder_type='CryptoPayment' AND recorder_version=$2)
 AND (SELECT count(*)=CASE WHEN $7::numeric>0 THEN 1 ELSE 0 END AND COALESCE(bool_and(record_type='receipt' AND merchant_id=$6 AND token_id=$4 AND amount=$7 AND reversal_of_line_id IS NULL),TRUE)
 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoPayment' AND recorder_version=$2)
 AND (SELECT count(*)=CASE WHEN $8::numeric>0 THEN 1 ELSE 0 END AND COALESCE(bool_and(record_type='receipt' AND merchant_id=$6 AND token_id=$4 AND amount=$8 AND fee_type='processing' AND reversal_of_line_id IS NULL),TRUE)
 FROM reg_crypto_fee_movements WHERE recorder_id=$1 AND recorder_type='CryptoPayment' AND recorder_version=$2)`, p.ID, p.PostedVersion, *p.ReceiptID, p.TokenID, p.Amount, p.MerchantID, net, fee).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("financial reorg requires the original complete payment movements")
	}
	return nil
}

// Version this small economic request separately from mutable observations.
// Confirmations, arrival times and proof refresh IDs are not financial identity.
func financialReorgRequestBasis(p *crypto_payment.CryptoPayment, event crypto.BlockchainEvent) ([]byte, error) {
	if !id.IsNil(event.NetworkID) && event.NetworkID != p.NetworkID {
		return nil, apperror.NewConflict("financial reorg event belongs to another network")
	}
	return json.Marshal(map[string]any{
		"schemaVersion": 1, "paymentId": p.ID, "networkId": p.NetworkID, "invoiceId": p.InvoiceID, "merchantId": p.MerchantID,
		"walletId": p.WalletID, "tokenId": p.TokenID, "txHash": p.TxHash, "eventKind": p.EventKind, "eventIndex": p.EventIndex,
		"amount": p.Amount.String(), "postedVersion": p.PostedVersion, "previousBlockNumber": p.BlockNumber, "previousBlockHash": p.BlockHash,
		"feeFixed": p.FeeFixed.String(), "feePercentBP": p.FeePercentBP, "feeMin": p.FeeMin.String(), "feeMax": p.FeeMax.String(),
		"observedBlockNumber": event.BlockNumber, "observedBlockHash": event.BlockHash, "observedTxHash": event.TxHash,
		"observedEventKind": event.EventKind, "observedEventIndex": event.EventIndex, "observedAmount": event.Amount.String(),
	})
}

func verifyFinancialReorgComplete(ctx context.Context, p *crypto_payment.CryptoPayment) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	for _, table := range []string{"reg_crypto_receipt_suspense_movements", "reg_crypto_merchant_balance_movements", "reg_crypto_fee_movements"} {
		var complete bool
		err := q.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM `+table+` m WHERE recorder_id=$1 AND recorder_type='CryptoPayment'
 AND recorder_version=$2 AND reversal_of_line_id IS NULL AND amount<>COALESCE((SELECT sum(r.amount) FROM `+table+` r WHERE r.reversal_of_line_id=m.line_id),0))`, p.ID, p.PostedVersion).Scan(&complete)
		if err != nil {
			return err
		}
		if !complete {
			return apperror.NewConflict("financial reorg compensation has incomplete financial movements")
		}
	}
	return nil
}
