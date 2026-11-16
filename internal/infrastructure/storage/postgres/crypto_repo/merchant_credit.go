package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
)

type MerchantCreditRepo struct {
	*crypto_document_repo.CryptoMerchantCreditRepo
}

func NewMerchantCreditRepo() *MerchantCreditRepo {
	return &MerchantCreditRepo{CryptoMerchantCreditRepo: crypto_document_repo.NewCryptoMerchantCreditRepo()}
}

// LockReceiptRemainder serializes every attribution of one receipt on its
// suspense balance row.
func (r *MerchantCreditRepo) LockReceiptRemainder(ctx context.Context, receiptID id.ID) (cryptoapp.ReceiptRemainder, error) {
	return lockReceiptRemainder(ctx, receiptID)
}

// lockReceiptRemainder is shared by every manual attribution of a receipt:
// merchant credits and platform fundings serialize on the same row.
func lockReceiptRemainder(ctx context.Context, receiptID id.ID) (cryptoapp.ReceiptRemainder, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return cryptoapp.ReceiptRemainder{}, apperror.NewValidation("receipt attribution requires a transaction")
	}
	result := cryptoapp.ReceiptRemainder{ReceiptID: receiptID}
	err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT d.wallet_id,s.token_id,s.amount,d.custody_merchant_id
		FROM reg_crypto_receipt_suspense_balances s JOIN doc_crypto_custody_receipts d ON d.id=s.receipt_id
		WHERE s.receipt_id=$1 AND d.posted FOR UPDATE OF s`, receiptID).
		Scan(&result.WalletID, &result.TokenID, &result.Amount, &result.CustodyMerchantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, apperror.NewNotFound("custody receipt", receiptID)
	}
	if err != nil {
		return result, fmt.Errorf("lock receipt remainder: %w", err)
	}
	// A payment keeps its receipt after a reorg unposts it; a payment_reversal
	// adjustment keeps it posted, so that remainder stays creditable.
	if err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM reg_crypto_custody_receipt_audits WHERE receipt_id=$1 AND status='orphaned'),
		EXISTS(SELECT 1 FROM doc_crypto_payments WHERE receipt_id=$1 AND NOT posted),
		EXISTS(SELECT 1 FROM doc_crypto_merchant_credits WHERE receipt_id=$1 AND status='prepared')
		  OR EXISTS(SELECT 1 FROM doc_crypto_platform_fundings WHERE receipt_id=$1 AND status='prepared')
		  OR EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE receipt_id=$1 AND status IN ('pending_signature','pending_approval'))`, receiptID).
		Scan(&result.Orphaned, &result.PaymentReorged, &result.AwaitingDecision); err != nil {
		return result, fmt.Errorf("read receipt canonicality: %w", err)
	}
	return result, nil
}

// ApprovalRequired follows the token policy; without a policy every credit
// needs a second operator.
func (r *MerchantCreditRepo) ApprovalRequired(ctx context.Context, tokenID id.ID, amount types.RawAmount) (bool, error) {
	var required bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT COALESCE(
		(SELECT $2::numeric>dual_approval_threshold FROM sys_crypto_receipt_credit_policies WHERE token_id=$1),TRUE)`,
		tokenID, amount).Scan(&required)
	if err != nil {
		return false, fmt.Errorf("read merchant credit policy: %w", err)
	}
	return required, nil
}

var _ cryptoapp.MerchantCreditRepository = (*MerchantCreditRepo)(nil)
