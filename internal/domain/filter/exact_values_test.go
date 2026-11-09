package filter_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/json"
	"metapus/internal/domain/filter"
)

func TestExactFiltersRejectLossyOrInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		kind   filter.ValueKind
		values []any
	}{
		{filter.MoneyValue, []any{123, float64(123), json.Number("123"), "1.0", "1e2", "+1", "01", "-0", "9223372036854775808", "-9223372036854775809"}},
		{filter.QuantityValue, []any{"0.00001", "1e2", json.Number("1e2"), ".1", "1.", " 1", "922337203685477.5808", "-922337203685477.5809", math.Inf(1), true}},
	} {
		for _, value := range tc.values {
			_, err := filter.Resolve(filter.Item{Field: "amount", Operator: filter.Equal, Value: value}, filter.FieldSpec{Kind: tc.kind})
			require.Error(t, err, "%v: %v", tc.kind, value)
			_, err = filter.Resolve(filter.Item{Field: "amount", Operator: filter.InList, Value: []any{"1", value}}, filter.FieldSpec{Kind: tc.kind})
			require.Error(t, err, "invalid list element: %v", value)
		}
		_, err := filter.Resolve(filter.Item{Field: "amount", Operator: filter.Contains, Value: "1"}, filter.FieldSpec{Kind: tc.kind})
		require.Error(t, err)
	}
}

func TestExactFilterListsAndRelatedNegativeSemantics(t *testing.T) {
	fields := map[string]filter.FieldSpec{"amount": {Kind: filter.MoneyValue}}
	_, err := filter.Resolve(filter.Item{Field: "amount", Operator: filter.Equal, Value: []string{"1"}}, fields["amount"])
	require.Error(t, err)
	_, err = filter.Resolve(filter.Item{Field: "amount", Operator: filter.InList, Value: "1"}, fields["amount"])
	require.Error(t, err)
}
