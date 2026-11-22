package crypto_worker

import (
	"testing"

	"metapus/pkg/sdk/types"
)

// Stand: PYUSD was already on chain while the ERC-20 scanner lagged, and the
// run opened a mismatch that took the wallet out of allocation for minutes.
func TestInFlightExplainsOnlyAnExactPositiveSurplus(t *testing.T) {
	amount := types.MustRawAmount
	cases := []struct {
		name                    string
		chain, ledger, inFlight string
		want                    bool
	}{
		{"pending payment", "1000000", "0", "1000000", true},
		{"pending on top of balance", "3000000", "2000000", "1000000", true},
		{"no in-flight transfer", "1000000", "0", "0", false},
		{"surplus beyond in-flight", "1500000", "0", "1000000", false},
		{"shortfall is never in-flight", "0", "1000000", "1000000", false},
	}
	for _, tc := range cases {
		if got := inFlightExplainsDelta(amount(tc.chain), amount(tc.ledger), amount(tc.inFlight)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
