package token

import (
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
)

func TestTokenDustThresholdAttribute(t *testing.T) {
	eth := NewToken("ETH-SEPOLIA", "Ethereum Sepolia ETH", id.New(), "ETH", 18, TokenStandardNative)
	_, configured, err := eth.DustThreshold()
	require.NoError(t, err)
	require.False(t, configured, "no attribute keeps the network default")

	eth.Attributes[DustThresholdAttribute] = "10000000000000" // 0.00001 ETH
	threshold, configured, err := eth.DustThreshold()
	require.NoError(t, err)
	require.True(t, configured)
	require.Equal(t, "10000000000000", threshold.String())
	require.NoError(t, eth.Validate(t.Context()))

	for _, invalid := range []any{1000, "-1", "1.5", "01"} {
		eth.Attributes[DustThresholdAttribute] = invalid
		require.ErrorContains(t, eth.Validate(t.Context()), DustThresholdAttribute, "%v", invalid)
	}
}
