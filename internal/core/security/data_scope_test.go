package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// dims is a helper to create a Dimensions map inline.
func dims(kvs ...string) map[string][]string {
	m := make(map[string][]string)
	for i := 0; i < len(kvs); i += 2 {
		key := kvs[i]
		m[key] = append(m[key], kvs[i+1])
	}
	return m
}

// dimCols is a helper to create a dimension→column map inline.
func dimCols(kvs ...string) map[string]string {
	m := make(map[string]string)
	for i := 0; i < len(kvs); i += 2 {
		m[kvs[i]] = kvs[i+1]
	}
	return m
}

func TestDataScope_CanAccessRecord(t *testing.T) {
	tests := []struct {
		name    string
		scope   *DataScope
		record  map[string]string
		want    bool
	}{
		{
			name:  "nil scope allows everything",
			scope: nil,
			record: map[string]string{"organization": "org-1"},
			want:   true,
		},
		{
			name:  "admin bypasses all",
			scope: &DataScope{IsAdmin: true},
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
			name: "empty dimension value in record — skipped",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization": {"org-1"},
			}},
			record: map[string]string{"organization": ""},
			want:   true,
		},
		{
			name: "three dimensions — all pass",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization":  {"org-1"},
				"counterparty":  {"cp-1"},
				"cost_article":  {"ca-opex", "ca-capex"},
			}},
			record: map[string]string{"organization": "org-1", "counterparty": "cp-1", "cost_article": "ca-opex"},
			want:   true,
		},
		{
			name: "three dimensions — cost_article denied",
			scope: &DataScope{Dimensions: map[string][]string{
				"organization":  {"org-1"},
				"counterparty":  {"cp-1"},
				"cost_article":  {"ca-opex"},
			}},
			record: map[string]string{"organization": "org-1", "counterparty": "cp-1", "cost_article": "ca-other"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.scope.CanAccessRecord(tt.record)
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

func TestDataScope_ApplyConditions(t *testing.T) {
	t.Run("nil scope — no conditions", func(t *testing.T) {
		var scope *DataScope
		conditions := scope.ApplyConditions(dimCols("organization", "organization_id"))
		assert.Nil(t, conditions)
	})

	t.Run("admin — no conditions", func(t *testing.T) {
		scope := &DataScope{IsAdmin: true, Dimensions: map[string][]string{"organization": {"org-1"}}}
		conditions := scope.ApplyConditions(dimCols("organization", "organization_id"))
		assert.Nil(t, conditions)
	})

	t.Run("one matching dimension", func(t *testing.T) {
		scope := &DataScope{Dimensions: map[string][]string{
			"organization": {"org-1", "org-2"},
		}}
		conditions := scope.ApplyConditions(dimCols("organization", "organization_id"))
		assert.Len(t, conditions, 1)
	})

	t.Run("two matching dimensions", func(t *testing.T) {
		scope := &DataScope{Dimensions: map[string][]string{
			"organization": {"org-1"},
			"counterparty": {"cp-1", "cp-2"},
		}}
		conditions := scope.ApplyConditions(dimCols(
			"organization", "organization_id",
			"counterparty", "supplier_id",
		))
		assert.Len(t, conditions, 2)
	})

	t.Run("dimension not in scope — no condition for it", func(t *testing.T) {
		scope := &DataScope{Dimensions: map[string][]string{
			"organization": {"org-1"},
		}}
		// Entity has counterparty column but user has no counterparty restriction
		conditions := scope.ApplyConditions(dimCols(
			"organization", "organization_id",
			"counterparty", "supplier_id",
		))
		assert.Len(t, conditions, 1) // only org
	})

	t.Run("empty dimColumns — no conditions", func(t *testing.T) {
		scope := &DataScope{Dimensions: map[string][]string{
			"organization": {"org-1"},
		}}
		conditions := scope.ApplyConditions(map[string]string{})
		assert.Len(t, conditions, 0)
	})

	t.Run("empty allowed IDs — impossible condition", func(t *testing.T) {
		scope := &DataScope{Dimensions: map[string][]string{
			"organization": {}, // present but empty = deny all
		}}
		conditions := scope.ApplyConditions(dimCols("organization", "organization_id"))
		assert.Len(t, conditions, 1) // impossible WHERE condition
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
