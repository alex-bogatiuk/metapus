package portalapp

import (
	"github.com/stretchr/testify/require"
	"metapus/pkg/sdk/types"
	"testing"
)

func TestReceiptChangeKeepsArbitraryPrecisionAndSignedChanges(t *testing.T) {
	for _, tt := range []struct{ current, previous, want string }{
		{"0", "0", "0.00"}, {"1", "0", "+100.00"}, {"0", "100", "-100.00"},
		{"200000000000000000000000000000000000000", "100000000000000000000000000000000000000", "+100.00"},
		{"1000000000000000001", "1000000000000000000", "0.00"},
	} {
		current, err := types.NewRawAmountFromString(tt.current)
		require.NoError(t, err)
		previous, err := types.NewRawAmountFromString(tt.previous)
		require.NoError(t, err)
		require.Equal(t, tt.want, ReceiptChangePercent(current, previous))
	}
}
