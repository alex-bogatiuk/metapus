package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNullableCryptoAmountScan(t *testing.T) {
	t.Parallel()

	var amount NullableCryptoAmount
	require.NoError(t, amount.Scan("115792089237316195423570985008687907853269984665640564039457584007913129639935"))
	require.True(t, amount.Valid)
	require.Equal(t, "115792089237316195423570985008687907853269984665640564039457584007913129639935", amount.Amount.String())

	require.NoError(t, amount.Scan(nil))
	require.False(t, amount.Valid)
	require.True(t, amount.Amount.IsZero())
}

func TestNullableCryptoAmountRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	var amount NullableCryptoAmount
	require.NoError(t, amount.Scan("1"))
	require.Error(t, amount.Scan("1.5"))
	require.False(t, amount.Valid)
	require.True(t, amount.Amount.IsZero())
}

func TestNullableCryptoAmountRejectsNilReceiver(t *testing.T) {
	t.Parallel()

	var amount *NullableCryptoAmount
	require.Error(t, amount.Scan("1"))
}
