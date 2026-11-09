package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFieldPolicy_IsFieldAllowed(t *testing.T) {
	tests := []struct {
		name     string
		policy   *FieldPolicy
		field    string
		expected bool
	}{
		{
			name:     "nil policy denies all",
			policy:   nil,
			field:    "any_field",
			expected: false,
		},
		{
			name:     "empty AllowedFields denies all",
			policy:   &FieldPolicy{AllowedFields: []string{}},
			field:    "any_field",
			expected: false,
		},
		{
			name:     "wildcard allows all",
			policy:   &FieldPolicy{AllowedFields: []string{"*"}},
			field:    "status",
			expected: true,
		},
		{
			name:     "wildcard with exclusion denies excluded",
			policy:   &FieldPolicy{AllowedFields: []string{"*", "-status", "-organization_id"}},
			field:    "status",
			expected: false,
		},
		{
			name:     "wildcard with exclusion allows non-excluded",
			policy:   &FieldPolicy{AllowedFields: []string{"*", "-status"}},
			field:    "name",
			expected: true,
		},
		{
			name:     "explicit list allows listed field",
			policy:   &FieldPolicy{AllowedFields: []string{"quantity", "unit_price"}},
			field:    "quantity",
			expected: true,
		},
		{
			name:     "explicit list denies unlisted field",
			policy:   &FieldPolicy{AllowedFields: []string{"quantity", "unit_price"}},
			field:    "status",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.policy.IsFieldAllowed(tt.field)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFieldPolicy_IsTablePartFieldAllowed(t *testing.T) {
	policy := &FieldPolicy{
		TableParts: map[string][]string{
			"items": {"quantity", "price"},
			"taxes": {"*"},
			"lines": {"*", "-unit_price", "-discount_amount"},
		},
	}

	tests := []struct {
		name     string
		part     string
		column   string
		expected bool
	}{
		{"allowed column", "items", "quantity", true},
		{"denied column", "items", "status", false},
		{"wildcard part", "taxes", "any_column", true},
		{"unknown part — open by default", "unknown", "col", true},
		{"wildcard with exclusion — allowed", "lines", "quantity", true},
		{"wildcard with exclusion — denied", "lines", "unit_price", false},
		{"wildcard with exclusion — denied 2", "lines", "discount_amount", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := policy.IsTablePartFieldAllowed(tt.part, tt.column)
			assert.Equal(t, tt.expected, result)
		})
	}

	t.Run("nil policy allows all", func(t *testing.T) {
		var p *FieldPolicy
		assert.True(t, p.IsTablePartFieldAllowed("items", "qty"))
	})

	t.Run("nil TableParts allows all", func(t *testing.T) {
		p := &FieldPolicy{AllowedFields: []string{"*"}}
		assert.True(t, p.IsTablePartFieldAllowed("items", "qty"))
	})
}

func TestFieldPolicy_ValidateFieldChanges(t *testing.T) {
	policy := &FieldPolicy{
		AllowedFields: []string{"*", "-organization_id", "-status"},
	}

	t.Run("no changes — passes", func(t *testing.T) {
		old := map[string]any{"name": "Test", "status": "draft"}
		new := map[string]any{"name": "Test", "status": "draft"}
		err := policy.ValidateFieldChanges(old, new)
		assert.NoError(t, err)
	})

	t.Run("allowed field changed — passes", func(t *testing.T) {
		old := map[string]any{"name": "Old", "status": "draft"}
		new := map[string]any{"name": "New", "status": "draft"}
		err := policy.ValidateFieldChanges(old, new)
		assert.NoError(t, err)
	})

	t.Run("restricted field changed — blocked", func(t *testing.T) {
		old := map[string]any{"name": "Test", "status": "draft"}
		new := map[string]any{"name": "Test", "status": "posted"}
		err := policy.ValidateFieldChanges(old, new)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status")
		assert.Contains(t, err.Error(), "read-only")
	})

	t.Run("restricted field unchanged — passes", func(t *testing.T) {
		old := map[string]any{"name": "Old", "organization_id": "org-1"}
		new := map[string]any{"name": "New", "organization_id": "org-1"}
		err := policy.ValidateFieldChanges(old, new)
		assert.NoError(t, err)
	})
}
