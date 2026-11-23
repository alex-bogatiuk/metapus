package automation

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

func TestSanitizePayloadNumbersConvertsOnlyExactInt64Values(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"plain":        json.Number("10000000"),
		"decimal":      json.Number("1000.0"),
		"exponent":     json.Number("1e3"),
		"fraction":     json.Number("1.25"),
		"max":          json.Number("9223372036854775807"),
		"aboveMax":     json.Number("9223372036854775808"),
		"belowMin":     json.Number("-9223372036854775809"),
		"huge":         json.Number("1e400"),
		"roundedFloat": float64(1 << 63),
		"nested":       []any{json.Number("-9223372036854775808"), json.Number("2.5")},
	}

	got := SanitizePayloadNumbers(payload)
	require.Equal(t, int64(10000000), got["plain"])
	require.Equal(t, int64(1000), got["decimal"])
	require.Equal(t, int64(1000), got["exponent"])
	require.Equal(t, 1.25, got["fraction"])
	require.Equal(t, int64(math.MaxInt64), got["max"])
	require.Equal(t, json.Number("9223372036854775808"), got["aboveMax"])
	require.Equal(t, json.Number("-9223372036854775809"), got["belowMin"])
	require.Equal(t, json.Number("1e400"), got["huge"])
	require.Equal(t, float64(1<<63), got["roundedFloat"])
	require.Equal(t, []any{int64(math.MinInt64), 2.5}, got["nested"])
}
