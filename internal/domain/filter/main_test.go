package filter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/domain/filter"
)

// --- M5: ExtractSearchQuery tests ---

func TestExtractSearchQuery_NoSearch(t *testing.T) {
	items := []filter.Item{
		{Field: "name", Operator: filter.Equal, Value: "foo"},
	}
	query, remaining := filter.ExtractSearchQuery(items)
	require.Empty(t, query)
	require.Len(t, remaining, 1)
}

func TestExtractSearchQuery_WithSearch(t *testing.T) {
	items := []filter.Item{
		{Field: "name", Operator: filter.Equal, Value: "foo"},
		{Field: "__search", Operator: filter.Contains, Value: "красн авто"},
		{Field: "code", Operator: filter.Equal, Value: "bar"},
	}
	query, remaining := filter.ExtractSearchQuery(items)
	require.Equal(t, "красн авто", query)
	require.Len(t, remaining, 2)
	for _, item := range remaining {
		require.NotEqual(t, "__search", item.Field)
	}
}

func TestExtractSearchQuery_NonStringValue(t *testing.T) {
	items := []filter.Item{
		{Field: "__search", Operator: filter.Contains, Value: 42},
	}
	query, remaining := filter.ExtractSearchQuery(items)
	require.Empty(t, query)
	require.Empty(t, remaining)
}

func TestValidateItems(t *testing.T) {
	tests := []struct {
		name    string
		give    []filter.Item
		wantErr bool
	}{
		{
			name: "valid equals",
			give: []filter.Item{
				{Field: "name", Operator: filter.Equal, Value: "foo"},
			},
			wantErr: false,
		},
		{
			name: "empty field",
			give: []filter.Item{
				{Field: "", Operator: filter.Equal, Value: "foo"},
			},
			wantErr: true,
		},
		{
			name: "unknown operator",
			give: []filter.Item{
				{Field: "name", Operator: "UNKNOWN", Value: "foo"},
			},
			wantErr: true,
		},
		{
			name: "missing value for value operator",
			give: []filter.Item{
				{Field: "name", Operator: filter.Equal, Value: nil},
			},
			wantErr: true,
		},
		{
			name: "is null without value is valid",
			give: []filter.Item{
				{Field: "name", Operator: filter.IsNull},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := filter.ValidateItems(tt.give)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
