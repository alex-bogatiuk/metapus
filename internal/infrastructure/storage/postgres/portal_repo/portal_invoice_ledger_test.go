package portal_repo

import (
	"github.com/stretchr/testify/require"
	"metapus/internal/core/types"
	"testing"
	"time"
)

func TestInvoiceLedgerGenerationsAndCompensations(t *testing.T) {
	amount := types.MustRawAmount("9007199254740993000000001")
	fee := types.MustRawAmount("1")
	net := amount.Sub(fee)
	payment := func(posted bool, version int, generations map[int]invoiceLedgerGeneration) *invoiceLedgerPayment {
		return &invoiceLedgerPayment{amount: amount, fee: fee, net: net, posted: posted, version: version, generations: generations}
	}
	original := func(currentNet, currentFee, reversed string) invoiceLedgerGeneration {
		return invoiceLedgerGeneration{gross: amount.String(), net: net.String(), fee: "1", currentGross: amount.String(), currentNet: currentNet, currentFee: currentFee, reversedNet: reversed}
	}
	cases := []struct {
		name                                   string
		p                                      *invoiceLedgerPayment
		status, credited, commission, reversed string
	}{
		{"not posted", payment(false, 0, nil), "known", "0", "0", "0"},
		{"posted", payment(true, 1, map[int]invoiceLedgerGeneration{1: original(net.String(), "1", "0")}), "known", net.String(), "1", "0"},
		{"partial adjustment", payment(true, 1, map[int]invoiceLedgerGeneration{1: original(net.Sub(types.NewRawAmountFromInt64(5)).String(), "0", "5")}), "known", net.Sub(types.NewRawAmountFromInt64(5)).String(), "0", "5"},
		{"reorg compensation", payment(false, 1, map[int]invoiceLedgerGeneration{1: original("0", "0", net.String())}), "known", "0", "0", net.String()},
		{"reposted", payment(true, 2, map[int]invoiceLedgerGeneration{1: original("0", "0", net.String()), 2: original(net.String(), "1", "0")}), "known", net.String(), "1", net.String()},
		{"missing generation", payment(true, 2, map[int]invoiceLedgerGeneration{1: original(net.String(), "1", "0")}), "inconsistent", "", "", ""},
		{"posted without roots", payment(true, 1, nil), "inconsistent", "", "", ""},
		{"incomplete original", payment(true, 1, map[int]invoiceLedgerGeneration{1: {gross: amount.String(), net: "0", fee: "1", currentGross: amount.String(), currentNet: "0", currentFee: "1", reversedNet: "0"}}), "inconsistent", "", "", ""},
		{"negative ledger", payment(true, 1, map[int]invoiceLedgerGeneration{1: original("-1", "0", "0")}), "inconsistent", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, total, err := summarizeInvoiceLedger(map[string]*invoiceLedgerPayment{"payment": tc.p})
			require.NoError(t, err)
			require.Equal(t, tc.status, total.Status)
			require.Equal(t, total, rows["payment"])
			if tc.status == "known" {
				require.Equal(t, tc.credited, *total.CreditedNetAmount)
				require.Equal(t, tc.commission, *total.ProcessingFeeAmount)
				require.Equal(t, tc.reversed, *total.ReversedNetAmount)
			} else {
				require.Nil(t, total.CreditedNetAmount)
				require.Nil(t, total.ProcessingFeeAmount)
				require.Nil(t, total.ReversedNetAmount)
			}
		})
	}
}
func TestInvoiceLedgerZeroFeeAndZeroNetAreKnown(t *testing.T) {
	for _, tc := range []struct{ fee, net string }{{"0", "10"}, {"10", "0"}} {
		_, total, err := summarizeInvoiceLedger(map[string]*invoiceLedgerPayment{"p": {amount: types.MustRawAmount("10"), fee: types.MustRawAmount(tc.fee), net: types.MustRawAmount(tc.net), posted: true, version: 1, generations: map[int]invoiceLedgerGeneration{1: {gross: "10", fee: tc.fee, net: tc.net, currentGross: "10", currentFee: tc.fee, currentNet: tc.net, reversedNet: "0"}}}})
		require.NoError(t, err)
		require.Equal(t, "known", total.Status)
		require.Equal(t, tc.net, *total.CreditedNetAmount)
	}
}
func TestInvoiceAcceptanceReason(t *testing.T) {
	now := time.Now()
	cases := []struct {
		status                    string
		accepts, single, selected bool
		expiry                    time.Time
		reason                    string
	}{
		{"created", true, false, false, now.Add(time.Hour), "open"},
		{"partially_paid", true, false, false, now.Add(-time.Hour), "open"},
		{"partially_paid", false, true, true, now.Add(time.Hour), "single_payment_selected"},
		{"underpaid", false, true, true, now.Add(time.Hour), "single_payment_selected"},
		{"underpaid", false, false, false, now.Add(-time.Hour), "expired"},
		{"underpaid", false, false, false, now.Add(time.Hour), "closed"},
		{"paid", false, true, true, now, "awaiting_confirmation"},
		{"confirmed", false, false, false, now, "awaiting_credit"},
		{"completed", false, false, false, now, "completed"},
		{"reversed", false, false, false, now, "reversed"},
		{"cancelled", false, false, false, now, "cancelled"},
		{"future_status", false, false, false, now, "closed"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.reason, invoicePaymentAcceptanceReason(tc.status, tc.accepts, tc.single, tc.selected, tc.expiry, now))
	}
}
