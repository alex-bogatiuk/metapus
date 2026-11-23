package types

import (
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestQuantityCheckedParserAndInt64Boundaries(t *testing.T) {
	for _, test := range []struct {
		text   string
		scaled int64
	}{
		{"0", 0}, {"1.0001", 10001}, {"-0.0001", -1},
		{"922337203685477.5807", math.MaxInt64}, {"-922337203685477.5808", math.MinInt64},
	} {
		for _, wire := range []string{test.text, `"` + test.text + `"`} {
			var q Quantity
			require.NoError(t, json.Unmarshal([]byte(wire), &q), wire)
			require.Equal(t, test.scaled, q.Int64Scaled())
			encoded, err := json.Marshal(q)
			require.NoError(t, err)
			var copy Quantity
			require.NoError(t, json.Unmarshal(encoded, &copy))
			require.Equal(t, q, copy)
		}
	}
	require.Equal(t, "-922337203685477.5808", Quantity(math.MinInt64).String())
}

func TestQuantityRejectsMalformedOrInexactInputWithoutMutation(t *testing.T) {
	for _, input := range []string{"", "null", "1e3", "1E-4", "1e30", "NaN", "Infinity", "1.00000", "1.0000junk", "1.-1", "+1", ".1", "1.", "1.2.3", " 1 ", "--1", "1844674407370956", "922337203685477.5808", "-922337203685477.5809"} {
		_, err := ParseQuantity(input)
		require.Error(t, err, input)
		encoded, err := json.Marshal(input)
		require.NoError(t, err)
		q := Quantity(123)
		require.Error(t, json.Unmarshal(encoded, &q), input)
		require.Equal(t, Quantity(123), q)
	}
	var q Quantity
	require.Error(t, json.Unmarshal([]byte("null"), &q))
}

func TestCheckedNumericConstructors(t *testing.T) {
	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN(), 1e30, -1e30} {
		_, err := CheckedQuantityFromFloat64(v)
		require.Error(t, err)
	}
	q, err := CheckedQuantityFromFloat64(1.23455)
	require.NoError(t, err)
	require.Equal(t, Quantity(12346), q)
	_, err = CheckedQuantityFromDecimal(decimal.RequireFromString("1.00001"))
	require.Error(t, err)
	for _, v := range []string{"9223372036854775808", "-9223372036854775809", "1.5"} {
		_, err := CheckedInt64FromDecimal(decimal.RequireFromString(v))
		require.Error(t, err)
	}
	for _, test := range []struct {
		major string
		scale int
		want  int64
	}{
		{"1.5", 0, 2}, {"-1.5", 0, -2}, {"1.2345", 3, 1235}, {"0.123456789", 9, 123456789},
		{"92233720368547758.07", 2, math.MaxInt64}, {"-92233720368547758.08", 2, math.MinInt64},
	} {
		result, err := CheckedMinorUnitsFromDecimal(decimal.RequireFromString(test.major), test.scale)
		require.NoError(t, err)
		require.EqualValues(t, test.want, result)
	}
	_, err = CheckedMinorUnitsFromDecimal(decimal.RequireFromString("92233720368547758.08"), 2)
	require.Error(t, err)
	_, err = CheckedAddInt64(math.MaxInt64, 1)
	require.Error(t, err)
	_, err = CheckedAddInt64(math.MinInt64, -1)
	require.Error(t, err)
}
