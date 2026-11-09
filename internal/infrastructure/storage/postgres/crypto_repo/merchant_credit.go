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
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

type MerchantCreditRepo struct {
	*document_repo.CryptoMerchantCreditRepo
}

func NewMerchantCreditRepo() *MerchantCreditRepo {
	return &MerchantCreditRepo{CryptoMerchantCreditRepo: document_repo.NewCryptoMerchantCreditRepo()}
}

// LockReceiptRemainder serializes every attribution of one receipt on its
// suspense balance row.
func (r *MerchantCreditRepo) LockReceiptRemainder(ctx context.Context, receiptID id.ID) (cryptoapp.ReceiptRemainder, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return cryptoapp.ReceiptRemainder{}, apperror.NewValidation("merchant credit requires a transaction")
	}
	result := cryptoapp.ReceiptRemainder{ReceiptID: receiptID}
	err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT s.token_id,s.amount,d.custody_merchant_id
		FROM reg_crypto_receipt_suspense_balances s JOIN doc_crypto_custody_receipts d ON d.id=s.receipt_id
		WHERE s.receipt_id=$1 AND d.posted FOR UPDATE OF s`, receiptID).
		Scan(&result.TokenID, &result.Amount, &result.CustodyMerchantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, apperror.NewNotFound("custody receipt", receiptID)
	}
	if err != nil {
		return result, fmt.Errorf("lock receipt remainder: %w", err)
	}
	return result, nil
}

// ApprovalRequired follows the token policy; without a policy every credit
// needs a second operator.
func (r *MerchantCreditRepo) ApprovalRequired(ctx context.Context, tokenID id.ID, amount types.CryptoAmount) (bool, error) {
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
