//go:build integration

package crypto_repo

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/types"
)

type receiptRefundFixture struct {
	merchantCreditFixture
	refunds  *ReceiptRefundRepo
	workflow *TreasuryWorkflowRepo
}

func newReceiptRefundFixture(t *testing.T) receiptRefundFixture {
	t.Helper()
	return receiptRefundFixture{merchantCreditFixture: newMerchantCreditFixture(t, false), refunds: NewReceiptRefundRepo(), workflow: NewTreasuryWorkflowRepo()}
}

func (f receiptRefundFixture) tx(fn func(ctx context.Context) error) error {
	return postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, fn)
}

// prepare records a destination and a pending refund of total, of which
// withheld stays with the platform for gas.
func (f receiptRefundFixture) prepare(t *testing.T, total, withheld int64, address string) (id.ID, error) {
	t.Helper()
	refundID, destinationID := id.New(), id.New()
	gasPayer := "platform"
	if withheld > 0 {
		gasPayer = "recipient"
	}
	err := f.tx(func(ctx context.Context) error {
		if err := f.workflow.CreateReceiptRefundDestination(ctx, cryptoapp.ReceiptRefundDestination{ID: destinationID, ReceiptID: f.receipt,
			NetworkID: f.request.Basis.NetworkID, Address: address, AddressKey: address, Evidence: "Sender signed a message with the address", CreatedBy: id.New()}); err != nil {
			return err
		}
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO doc_treasury_withdrawals
			(id,number,token_id,source_wallet_id,dest_address,address_id,amount,status,purpose,receipt_id,gas_payer,withheld_amount,description,created_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,'pending_approval','receipt_refund',$8,$9,$10,'Sent by mistake',$11)`,
			refundID, "TW-"+id.Compact(refundID), f.request.Basis.TokenID, f.request.Basis.WalletID, address, destinationID,
			total-withheld, f.receipt, gasPayer, withheld, id.New())
		return err
	})
	return refundID, err
}

func (f receiptRefundFixture) setStatus(ctx context.Context, refundID id.ID, status string) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE doc_treasury_withdrawals SET status=$2 WHERE id=$1`, refundID, status)
	return err
}

func (f receiptRefundFixture) payable(t *testing.T) string {
	t.Helper()
	var amount string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COALESCE((SELECT amount::text FROM reg_crypto_refund_payable_balances WHERE receipt_id=$1),'0')`, f.receipt).Scan(&amount))
	return amount
}

func TestReceiptRefundMovesSuspenseToPayableOnApprovalAndBackOnCancel(t *testing.T) {
	f := newReceiptRefundFixture(t)
	gross := f.request.Basis.Amount
	total := gross.BigInt().Int64()
	require.Greater(t, total, int64(2), "the fixture receipt must cover the withheld gas")

	_, err := f.prepare(t, total, 0, strings.ToLower(f.request.Event.ToAddress))
	require.ErrorContains(t, err, "external to managed custody", "a refund never pays a managed wallet")
	_, err = f.prepare(t, total+1, 0, "0x1111111111111111111111111111111111111111")
	require.ErrorContains(t, err, "exceeds the receipt remainder")

	refundID, err := f.prepare(t, total, 2, "0x1111111111111111111111111111111111111111")
	require.NoError(t, err)
	suspense, _, _ := f.amounts(t)
	require.Equal(t, gross.String(), suspense, "a prepared refund moves nothing")
	require.Equal(t, "0", f.payable(t))

	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.ErrorContains(t, err, "awaits a decision", "a credit cannot race a pending refund")
	_, err = f.prepare(t, 1, 0, "0x2222222222222222222222222222222222222222")
	require.ErrorContains(t, err, "awaits a decision", "one refund decision at a time")
	open := f.journal(t, cryptoapp.ReceiptJournalOpen, false)
	require.Equal(t, 1, open.Total)
	require.Equal(t, "awaiting_approval", open.Items[0].State)
	require.Equal(t, []string{refundID.String(), "pending_approval", types.NewRawAmountFromInt64(total - 2).String(), "2"},
		[]string{*open.Items[0].RefundID, *open.Items[0].RefundStatus, *open.Items[0].RefundAmount, *open.Items[0].RefundWithheld})

	// Approval without moving the money is refused at commit.
	err = f.tx(func(ctx context.Context) error { return f.setStatus(ctx, refundID, "approved") })
	require.ErrorContains(t, err, "approved receipt refund requires an open payable")

	approve := func(ctx context.Context) error {
		if err := f.refunds.OpenPayable(ctx, refundID); err != nil {
			return err
		}
		if err := f.refunds.OpenPayable(ctx, refundID); err != nil {
			return err
		}
		return f.setStatus(ctx, refundID, "approved")
	}
	require.NoError(t, f.tx(approve), "opening the payable is idempotent for the executor")
	suspense, _, _ = f.amounts(t)
	require.Equal(t, "0", suspense, "suspense gives up the amount and the withheld gas")
	require.Equal(t, types.NewRawAmountFromInt64(total-2).String(), f.payable(t), "the payable owes what the payer receives")

	// A cancellation must give the money back to suspense.
	err = f.tx(func(ctx context.Context) error { return f.setStatus(ctx, refundID, "rejected") })
	require.ErrorContains(t, err, "cancelled receipt refund requires suspense restored")
	require.NoError(t, f.tx(func(ctx context.Context) error {
		if err := f.refunds.Restore(ctx, refundID); err != nil {
			return err
		}
		if err := f.refunds.Restore(ctx, refundID); err != nil {
			return err
		}
		return f.setStatus(ctx, refundID, "rejected")
	}))
	suspense, _, _ = f.amounts(t)
	require.Equal(t, gross.String(), suspense)
	require.Equal(t, "0", f.payable(t))
	open = f.journal(t, cryptoapp.ReceiptJournalOpen, false)
	require.Equal(t, "unattributed", open.Items[0].State)
	require.Equal(t, "rejected", *open.Items[0].RefundStatus)

	_, err = f.pool.Exec(f.ctx, `UPDATE doc_treasury_withdrawals SET withheld_amount=0,gas_payer='platform' WHERE id=$1`, refundID)
	require.ErrorContains(t, err, "refund terms are immutable")
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_refund_payable_movements WHERE recorder_id=$1`, refundID)
	require.ErrorContains(t, err, "append-only")
}

func TestReceiptRefundPaidOutClosesThePayable(t *testing.T) {
	f := newReceiptRefundFixture(t)
	total := f.request.Basis.Amount.BigInt().Int64()
	refundID, err := f.prepare(t, total, 0, "0x3333333333333333333333333333333333333333")
	require.NoError(t, err)
	require.NoError(t, f.tx(func(ctx context.Context) error {
		if err := f.refunds.OpenPayable(ctx, refundID); err != nil {
			return err
		}
		return f.setStatus(ctx, refundID, "approved")
	}))
	err = f.tx(func(ctx context.Context) error { return f.setStatus(ctx, refundID, "confirmed") })
	require.ErrorContains(t, err, "paid receipt refund requires its payable closed")
	// Recognition of the payout closes the payable; the suspense stays given up.
	require.NoError(t, f.tx(func(ctx context.Context) error {
		if _, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_refund_payable_movements
			(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
			SELECT id,'TreasuryWithdrawal',2,NOW(),'expense',receipt_id,token_id,amount FROM doc_treasury_withdrawals WHERE id=$1`, refundID); err != nil {
			return err
		}
		return f.setStatus(ctx, refundID, "confirmed")
	}))
	suspense, _, _ := f.amounts(t)
	require.Equal(t, "0", suspense)
	require.Equal(t, "0", f.payable(t))
	err = f.tx(func(ctx context.Context) error { return f.refunds.Restore(ctx, refundID) })
	require.NoError(t, err, "a closed payable has nothing to restore")
	suspense, _, _ = f.amounts(t)
	require.Equal(t, "0", suspense)
}
