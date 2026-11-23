package filter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/domain/filter"
)

func TestResolveRejectsInvalidFieldKind(t *testing.T) {
	tests := []struct {
		name string
		give filter.FieldSpec
	}{
		{name: "undeclared kind", give: filter.FieldSpec{}},
		{name: "out of range kind", give: filter.FieldSpec{Kind: 255}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, item := range []filter.Item{
				{Field: "value", Operator: filter.Equal, Value: "1"},
				{Field: "value", Operator: filter.IsNull},
				{Field: "value", Operator: filter.IsNotNull},
			} {
				t.Run(string(item.Operator), func(t *testing.T) {
					_, err := filter.Resolve(item, tt.give)
					require.ErrorContains(t, err, "unsupported filter field kind")
				})
			}
		})
	}
}

func TestResolveDeclaredFieldKinds(t *testing.T) {
	tests := []struct {
		name      string
		giveKind  filter.ValueKind
		giveValue string
		wantKind  filter.ValueKind
		wantValue any
	}{
		{"native", filter.NativeValue, "1.25", filter.NativeValue, "1.25"},
		{"date", filter.DateValue, "2026-09-21", filter.DateValue, "2026-09-21"},
		{"quantity", filter.QuantityValue, "1.25", filter.QuantityValue, int64(12500)},
		{"money", filter.MoneyValue, "125", filter.MoneyValue, int64(125)},
		{"crypto", filter.RawAmountValue, "9007199254740993", filter.RawAmountValue, "9007199254740993"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := filter.Resolve(
				filter.Item{Field: "value", Operator: filter.Equal, Value: tt.giveValue},
				filter.FieldSpec{Kind: tt.giveKind},
			)
			require.NoError(t, err)
			require.Equal(t, tt.wantKind, resolved.Kind())
			require.Equal(t, tt.wantValue, resolved.Value())
			require.Equal(t, "value", resolved.Field())
			require.Equal(t, filter.Equal, resolved.Operator())
		})
	}
}
