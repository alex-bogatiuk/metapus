package portal_repo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/types"
)

func TestInvoicePaymentSummaryUsesEveryCurrentPaymentAndEachFeeSnapshot(t *testing.T) {
	amount := types.NewRawAmountFromInt64
	zero := types.ZeroRawAmount()
	summary := &invoicePaymentSummary{Fee: zero, Net: zero}
	summary.addPayment(amount(1000000), zero, 0, zero, zero, "settled", "canonical")
	summary.addPayment(amount(9000000), zero, 0, zero, zero, "settled", "canonical")
	summary.addPayment(amount(2000000), zero, 0, zero, zero, "reorged", "orphaned")
	require.Equal(t, 3, summary.Count)
	require.Equal(t, "0", summary.Fee.String())
	require.Equal(t, "10000000", summary.Net.String())

	withMinimum := &invoicePaymentSummary{Fee: zero, Net: zero}
	withMinimum.addPayment(amount(1000000), zero, 0, amount(100000), zero, "settled", "canonical")
	withMinimum.addPayment(amount(9000000), zero, 0, amount(100000), zero, "settled", "canonical")
	require.Equal(t, "200000", withMinimum.Fee.String())
	require.Equal(t, "9800000", withMinimum.Net.String())
}
