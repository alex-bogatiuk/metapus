package withdrawal_request

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

func TestRefundBasisRequiresPaymentAndReason(t *testing.T) {
	ctx := context.Background()
	newRequest := func() *WithdrawalRequest {
		return NewWithdrawalRequest(id.New(), id.New(), id.New(), "destination", types.NewRawAmountFromInt64(10))
	}

	plain := newRequest()
	require.NoError(t, plain.Validate(ctx))
	require.False(t, plain.IsRefund())

	refund := newRequest()
	refund.MarkAsRefund(id.New(), "  Order cancelled  ")
	require.NoError(t, refund.Validate(ctx))
	require.True(t, refund.IsRefund())
	require.Equal(t, "Order cancelled", refund.Description)

	noReason := newRequest()
	noReason.MarkAsRefund(id.New(), "   ")
	require.ErrorContains(t, noReason.Validate(ctx), "refund reason is required")

	longReason := newRequest()
	longReason.MarkAsRefund(id.New(), strings.Repeat("я", 251))
	require.ErrorContains(t, longReason.Validate(ctx), "exceeds 500 bytes")

	noPayment := newRequest()
	noPayment.BasisType = RefundBasisType
	noPayment.Description = "reason"
	require.ErrorContains(t, noPayment.Validate(ctx), "refund requires the payment")

	otherBasis := newRequest()
	paymentID := id.New()
	otherBasis.BasisType, otherBasis.BasisID = "CryptoInvoice", &paymentID
	require.ErrorContains(t, otherBasis.Validate(ctx), "basis must be a credited payment")

	untyped := newRequest()
	untyped.BasisID = &paymentID
	require.ErrorContains(t, untyped.Validate(ctx), "basis type is required")
}
