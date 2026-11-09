package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataScope_CanAccessRecord(t *testing.T) {
	tests := []struct {
		name   string
		scope  *DataScope
		record map[string]string
		want   bool
	}{
		{
			name:   "nil scope allows everything",
			scope:  nil,
			record: map[string]string{"organization": "org-1"},
			want:   true,
		},
		{
			name:   "admin bypasses all",
			scope:  &DataScope{IsAdmin: true},
			record: map[string]string{"organization": "org-1"},
			want:   true,
		},
		{
			name: "allowed org — granted",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1", "org-2"},
			}},
			record: map[string]string{"organization": "org-1"},
			want:   true,
		},
		{
			name: "denied org — blocked",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1"},
			}},
			record: map[string]string{"organization": "org-3"},
			want:   false,
		},
		{
			name: "allowed counterparty — granted",
			scope: &DataScope{Dimensions: map[string][]string{
				"counterparty": {"cp-1", "cp-2"},
			}},
			record: map[string]string{"counterparty": "cp-2"},
			want:   true,
		},
		{
			name: "denied counterparty — blocked",
			scope: &DataScope{Dimensions: map[string][]string{
				"counterparty": {"cp-1"},
			}},
			record: map[string]string{"counterparty": "cp-99"},
			want:   false,
		},
		{
			name: "dimension not in scope — no restriction",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1"},
			}},
			record: map[string]string{"organization": "org-1", "counterparty": "any-cp"},
			want:   true,
		},
		{
			name: "both dimensions ok — granted",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1"},
				"counterparty": {"cp-1"},
			}},
			record: map[string]string{"organization": "org-1", "counterparty": "cp-1"},
			want:   true,
		},
		{
			name: "org ok but counterparty denied",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1"},
				"counterparty": {"cp-1"},
			}},
			record: map[string]string{"organization": "org-1", "counterparty": "cp-2"},
			want:   false,
		},
		{
			name: "empty dimension value in record — denied",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1"},
			}},
			record: map[string]string{"organization": ""},
			want:   false,
		},
		{
			name: "three dimensions — all pass",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1"},
				"counterparty": {"cp-1"},
				"cost_article": {"ca-opex", "ca-capex"},
			}},
			record: map[string]string{"organization": "org-1", "counterparty": "cp-1", "cost_article": "ca-opex"},
			want:   true,
		},
		{
			name: "three dimensions — cost_article denied",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1"},
				"counterparty": {"cp-1"},
				"cost_article": {"ca-opex"},
			}},
			record: map[string]string{"organization": "org-1", "counterparty": "cp-1", "cost_article": "ca-other"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.scope.CanAccessRecord("", tt.record)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDataScope_CanMutate(t *testing.T) {
	t.Run("nil scope — allows mutation", func(t *testing.T) {
		var scope *DataScope
		assert.NoError(t, scope.CanMutate())
	})

	t.Run("non-readonly — allows", func(t *testing.T) {
		scope := &DataScope{ReadOnly: false}
		assert.NoError(t, scope.CanMutate())
	})

	t.Run("readonly — denies", func(t *testing.T) {
		scope := &DataScope{ReadOnly: true}
		err := scope.CanMutate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "read-only")
	})
}

func TestDataScope_SetDimension(t *testing.T) {
	scope := &DataScope{}
	scope.SetDimension("cost_article", []string{"ca-1", "ca-2"})
	scope.SetDimension("organization", []string{"org-1"})

	assert.Equal(t, []string{"ca-1", "ca-2"}, scope.Dimensions["cost_article"])
	assert.Equal(t, []string{"org-1"}, scope.Dimensions["organization"])

	// Replace existing
	scope.SetDimension("organization", []string{"org-new"})
	assert.Equal(t, []string{"org-new"}, scope.Dimensions["organization"])
}
