package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNullableRawAmountScan(t *testing.T) {
	t.Parallel()

	var amount NullableRawAmount
	require.NoError(t, amount.Scan("115792089237316195423570985008687907853269984665640564039457584007913129639935"))
	require.True(t, amount.Valid)
	require.Equal(t, "115792089237316195423570985008687907853269984665640564039457584007913129639935", amount.Amount.String())

	require.NoError(t, amount.Scan(nil))
	require.False(t, amount.Valid)
	require.True(t, amount.Amount.IsZero())
}

func TestNullableRawAmountRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	var amount NullableRawAmount
	require.NoError(t, amount.Scan("1"))
	require.Error(t, amount.Scan("1.5"))
	require.False(t, amount.Valid)
	require.True(t, amount.Amount.IsZero())
}

func TestNullableRawAmountRejectsNilReceiver(t *testing.T) {
	t.Parallel()

	var amount *NullableRawAmount
	require.Error(t, amount.Scan("1"))
}
