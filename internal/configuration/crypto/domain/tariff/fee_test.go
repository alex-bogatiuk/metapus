package tariff

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/types"
)

func amount(v int64) types.RawAmount { return types.NewRawAmountFromInt64(v) }

func TestFeeClampsAndSplits(t *testing.T) {
	fee := Fee{Fixed: amount(10), PercentBP: 100, Min: amount(15), Max: amount(50)}
	require.True(t, fee.Amount(amount(100)).Equal(amount(15)), "floor")
	require.True(t, fee.Amount(amount(2000)).Equal(amount(30)))
	require.True(t, fee.Amount(amount(100000)).Equal(amount(50)), "cap")

	charged, net := fee.Split(amount(2000))
	require.True(t, charged.Equal(amount(30)))
	require.True(t, net.Equal(amount(1970)))

	charged, net = Fee{Fixed: amount(20)}.Split(amount(20))
	require.True(t, charged.Equal(amount(20)))
	require.True(t, net.IsZero(), "a fee equal to the gross credits nothing")
	_, net = Fee{Fixed: amount(30)}.Split(amount(20))
	require.True(t, net.IsZero())
}
