package treasury_withdrawal

import (
	"context"
	"testing"

	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/pkg/sdk/appctx"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func TestTreasuryWithdrawalAdminStillCannotApproveOwnOperation(t *testing.T) {
	actor := id.New()
	doc := NewTreasuryWithdrawal(id.New(), id.New(), id.New(), "external", types.NewRawAmountFromInt64(1))
	doc.CreatedBy = actor
	doc.ApprovedBy = &actor
	ctx := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: actor.String(), IsAdmin: true, Roles: []string{"admin"}})
	if err := doc.Validate(ctx); err == nil {
		t.Fatal("provider opening exception must not apply to treasury")
	}
}

func TestTreasuryWithdrawalPostingCreatesNoPrincipalOrRevenueExpense(t *testing.T) {
	// Principal and revenue are recognized only at final chain settlement.
	var doc any = NewTreasuryWithdrawal(id.New(), id.New(), id.New(), "external", types.NewRawAmountFromInt64(23))
	_, balance := doc.(cryptoposting.CryptoBalanceMovementSource)
	_, fee := doc.(cryptoposting.CryptoFeeMovementSource)
	require.False(t, balance)
	require.False(t, fee)
}

func TestTreasuryWithdrawalCanPostRequiresBroadcastTxHash(t *testing.T) {
	doc := NewTreasuryWithdrawal(
		id.New(),
		id.New(),
		id.New(),
		"T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb",
		types.NewRawAmountFromInt64(100),
	)

	if err := doc.CanPost(context.Background()); err == nil {
		t.Fatal("pending treasury withdrawal must not be postable")
	}

	doc.Status = StatusBroadcast
	if err := doc.CanPost(context.Background()); err == nil {
		t.Fatal("broadcast treasury withdrawal without tx hash must not be postable")
	}

	doc.TxHash = "tx_hash"
	if err := doc.CanPost(context.Background()); err != nil {
		t.Fatalf("broadcast treasury withdrawal with tx hash should be postable: %v", err)
	}
}

func TestReceiptRefundTermsFollowTheGasPayer(t *testing.T) {
	receipt, token, wallet, destination := id.New(), id.New(), id.New(), id.New()
	amount := types.NewRawAmountFromInt64
	refund, err := NewReceiptRefund(receipt, token, wallet, destination, "payer", amount(25), amount(2), GasPayerRecipient, " Sent by mistake ")
	require.NoError(t, err)
	require.True(t, refund.IsReceiptRefund())
	require.Equal(t, "23", refund.Amount.String(), "the payer receives the refund less the withheld gas")
	require.Equal(t, "25", refund.RefundTotal().String(), "suspense gives up the whole refund")
	require.Equal(t, "Sent by mistake", refund.Description)
	require.NoError(t, refund.Validate(context.Background()))

	_, err = NewReceiptRefund(receipt, token, wallet, destination, "payer", amount(25), amount(25), GasPayerRecipient, "reason")
	require.ErrorContains(t, err, "below the refund amount")
	_, err = NewReceiptRefund(receipt, token, wallet, destination, "payer", amount(25), amount(1), GasPayerPlatform, "reason")
	require.ErrorContains(t, err, "platform-paid gas withholds nothing")
	_, err = NewReceiptRefund(receipt, token, wallet, destination, "payer", amount(25), types.ZeroRawAmount(), GasPayerRecipient, "reason")
	require.ErrorContains(t, err, "requires the withheld amount")
	_, err = NewReceiptRefund(receipt, token, wallet, destination, "payer", amount(25), types.ZeroRawAmount(), GasPayerPlatform, "  ")
	require.ErrorContains(t, err, "refund reason is required")

	revenue := NewTreasuryWithdrawal(token, wallet, destination, "treasury", amount(5))
	require.NoError(t, revenue.Validate(context.Background()))
	revenue.WithheldAmount = amount(1)
	require.ErrorContains(t, revenue.Validate(context.Background()), "carries no refund terms")
}
