package jsoncodec

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/filter"
)

func TestExactRequestBindingPreservesFilterNumbersAndStrictMoney(t *testing.T) {
	type request struct {
		Filters []filter.Item    `json:"filters"`
		Amount  types.MinorUnits `json:"amount"`
	}
	var input request
	err := DecodeRequestPreserveNumbers(strings.NewReader(`{"filters":[{"field":"quantity","operator":"eq","value":922337203685477.5807},{"field":"quantity","operator":"eq","value":1e2}],"amount":"9007199254740993"}`), &input)
	require.NoError(t, err)
	require.Equal(t, json.Number("922337203685477.5807"), input.Filters[0].Value)
	require.Equal(t, types.MinorUnits(9007199254740993), input.Amount)
	_, err = filter.Resolve(input.Filters[0], filter.FieldSpec{Kind: filter.QuantityValue})
	require.NoError(t, err)
	_, err = filter.Resolve(input.Filters[1], filter.FieldSpec{Kind: filter.QuantityValue})
	require.Error(t, err, "the number's exponent must reach the strict quantity parser")
	for _, body := range []string{
		`{"amount":1}`, `{"amount":"1","extra":true}`, `{"amount":"1"} {}`,
		`{"filters":[{"field":"quantity","operator":"eq","value":1,"extra":true}]}`,
	} {
		require.Error(t, DecodeRequestPreserveNumbers(strings.NewReader(body), &request{}), body)
	}
}
