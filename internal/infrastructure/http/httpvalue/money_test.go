package httpvalue

import (
	"math"
	"strconv"
	"testing"

	"metapus/internal/core/json"

	"github.com/stretchr/testify/require"
)

func TestMoneyWireExactRoundTrip(t *testing.T) {
	for _, value := range []int64{0, 1, -1, 9007199254740991, 9007199254740992, 9007199254740993, math.MaxInt64, math.MinInt64} {
		t.Run(strconv.FormatInt(value, 10), func(t *testing.T) {
			encoded, err := json.Marshal(MinorUnits(value))
			require.NoError(t, err)
			require.Equal(t, strconv.Quote(strconv.FormatInt(value, 10)), string(encoded))
			var decoded MinorUnits
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			require.Equal(t, MinorUnits(value), decoded)
		})
	}
}

func TestMoneyWireRejectsAmbiguousOrOutOfRangeValues(t *testing.T) {
	for _, value := range []string{`0`, `9007199254740993`, `null`, `true`, `[]`, `{}`, `""`, `"+1"`, `"01"`, `"-0"`, `"-01"`, `"1.0"`, `"1e2"`, `" 1"`, `"1 "`, `"9223372036854775808"`, `"-9223372036854775809"`} {
		t.Run(value, func(t *testing.T) {
			decoded := MinorUnits(42)
			require.Error(t, json.Unmarshal([]byte(value), &decoded))
			require.Equal(t, MinorUnits(42), decoded, "invalid input must not replace the amount")
		})
	}
}

func TestMoneyWireOptionalFieldsUseFacadeSemantics(t *testing.T) {
	type response struct {
		Amount   MinorUnits  `json:"amount"`
		Zero     MinorUnits  `json:"zero,omitzero"`
		Empty    MinorUnits  `json:"empty,omitempty"`
		Optional *MinorUnits `json:"optional,omitempty"`
		Nullable *MinorUnits `json:"nullable"`
	}
	data, err := json.Marshal(response{})
	require.NoError(t, err)
	// A quoted zero is non-empty in the v2 facade. Only omitzero omits it.
	require.JSONEq(t, `{"amount":"0","empty":"0","nullable":null}`, string(data))
}
