package aml

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

func TestSplitInventoryConservesPrincipalAndFeeAcrossPartialTransfers(t *testing.T) {
	remaining := InventoryPartition{types.MustRawAmount("2"), types.MustRawAmount("1")}
	first, err := SplitInventory(remaining, types.MustRawAmount("1"))
	require.NoError(t, err)
	require.Equal(t, "0", first.Principal.String(), "indivisible raw unit must not round principal up")
	require.Equal(t, "1", first.ServiceFee.String())
	remaining.Principal = remaining.Principal.Sub(first.Principal)
	remaining.ServiceFee = remaining.ServiceFee.Sub(first.ServiceFee)
	second, err := SplitInventory(remaining, remaining.Gross())
	require.NoError(t, err)
	require.Equal(t, "2", second.Principal.String())
	require.True(t, second.ServiceFee.IsZero())
	require.Equal(t, "3", first.Gross().Add(second.Gross()).String())
	_, err = SplitInventory(remaining, types.MustRawAmount("3"))
	require.ErrorContains(t, err, "insufficient")
	_, err = SplitInventory(remaining, types.ZeroRawAmount())
	require.Error(t, err)
}

func TestSplitInventoryRetainsRawUnitPrecisionAtNumeric78Limit(t *testing.T) {
	// A decimal division rounded before floor would incorrectly allocate the
	// near-integer principal. Both Go and PostgreSQL must use integer quotient.
	principal := types.MustRawAmount("999999999999999999999999999999999999999999999999999999999999999999999999999998")
	available := InventoryPartition{Principal: principal, ServiceFee: types.MustRawAmount("1")}
	split, err := SplitInventory(available, principal)
	require.NoError(t, err)
	require.Equal(t, principal.Sub(types.MustRawAmount("1")).String(), split.Principal.String())
	require.Equal(t, "1", split.ServiceFee.String())
	require.Equal(t, principal.String(), split.Gross().String())
	full, err := SplitInventory(available, available.Gross())
	require.NoError(t, err)
	require.Equal(t, available.Principal.String(), full.Principal.String())
	require.Equal(t, available.ServiceFee.String(), full.ServiceFee.String())
}
