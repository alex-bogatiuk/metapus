package crypto_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// ReceiptRefundRepo moves a receipt refund between receipt suspense and refund
// payable. The payout itself is recognised by TreasuryCustodyRepo, which closes
// the payable; fn_crypto_assert_receipt_refund checks every line set.
type ReceiptRefundRepo struct{}

func NewReceiptRefundRepo() *ReceiptRefundRepo { return &ReceiptRefundRepo{} }

// IsReceiptRefund reports whether a treasury withdrawal returns a receipt.
func (*ReceiptRefundRepo) IsReceiptRefund(ctx context.Context, withdrawalID id.ID) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var purpose string
	if err := q.QueryRow(ctx, `SELECT purpose FROM doc_treasury_withdrawals WHERE id=$1`, withdrawalID).Scan(&purpose); err != nil {
		return false, fmt.Errorf("read treasury withdrawal purpose: %w", err)
	}
	return purpose == string(treasury_withdrawal.PurposeReceiptRefund), nil
}

// OpenPayable is the approval of a receipt refund: suspense
// gives up amount+withheld, the payable owes the amount the payer receives. A
// competing attribution of the same remainder fails on the suspense balance.
// Idempotent for the executor, which repeats the reservation step.
func (*ReceiptRefundRepo) OpenPayable(ctx context.Context, withdrawalID id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var opened bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_refund_payable_movements WHERE recorder_id=$1 AND recorder_version=1)`, withdrawalID).Scan(&opened); err != nil {
		return fmt.Errorf("read receipt refund payable: %w", err)
	}
	if opened {
		return nil
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_receipt_suspense_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
		SELECT id,'TreasuryWithdrawal',1,NOW(),'expense',receipt_id,token_id,amount+withheld_amount FROM doc_treasury_withdrawals WHERE id=$1 AND purpose='receipt_refund'
	`, withdrawalID); err != nil {
		return fmt.Errorf("take receipt refund out of suspense: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_refund_payable_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
		SELECT id,'TreasuryWithdrawal',1,NOW(),'receipt',receipt_id,token_id,amount FROM doc_treasury_withdrawals WHERE id=$1 AND purpose='receipt_refund'
	`, withdrawalID); err != nil {
		return fmt.Errorf("recognise receipt refund payable: %w", err)
	}
	return nil
}

// Restore cancels an approved refund that paid nothing: the
// payable closes and suspense takes the whole amount back. Nothing moved
// before approval, so a refund rejected earlier needs no lines.
func (*ReceiptRefundRepo) Restore(ctx context.Context, withdrawalID id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var open bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_refund_payable_movements WHERE recorder_id=$1 AND recorder_version=1)
		AND NOT EXISTS(SELECT 1 FROM reg_crypto_refund_payable_movements WHERE recorder_id=$1 AND recorder_version=2)`, withdrawalID).Scan(&open); err != nil {
		return fmt.Errorf("read receipt refund payable: %w", err)
	}
	if !open {
		return nil
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_refund_payable_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
		SELECT id,'TreasuryWithdrawal',2,NOW(),'expense',receipt_id,token_id,amount FROM doc_treasury_withdrawals WHERE id=$1
	`, withdrawalID); err != nil {
		return fmt.Errorf("close cancelled receipt refund payable: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_receipt_suspense_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount,reversal_of_line_id,reversal_reason)
		SELECT recorder_id,'TreasuryWithdrawalReversal',1,NOW(),'receipt',receipt_id,token_id,amount,line_id,'receipt refund cancelled before payout'
		FROM reg_crypto_receipt_suspense_movements WHERE recorder_id=$1 AND recorder_type='TreasuryWithdrawal'
	`, withdrawalID); err != nil {
		return fmt.Errorf("return cancelled receipt refund to suspense: %w", err)
	}
	return nil
}
