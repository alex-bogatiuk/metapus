package readpolicy

import (
	"github.com/stretchr/testify/assert"
	"metapus/internal/core/security"
	"testing"
)

func dimCols(kvs ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i < len(kvs); i += 2 {
		m[kvs[i]] = kvs[i+1]
	}
	return m
}
func TestDataScope_ApplyConditions(t *testing.T) {
	t.Run("nil scope — no conditions", func(t *testing.T) {
		var scope *security.DataScope
		conditions := ApplyConditions(scope, "", dimCols("organization", "organization_id"))
		assert.Nil(t, conditions)
	})

	t.Run("admin — no conditions", func(t *testing.T) {
		scope := &security.DataScope{IsAdmin: true, Dimensions: map[string][]string{"organization": {"org-1"}}}
		conditions := ApplyConditions(scope, "", dimCols("organization", "organization_id"))
		assert.Nil(t, conditions)
	})

	t.Run("one matching dimension", func(t *testing.T) {
		scope := &security.DataScope{Dimensions: map[string][]string{
			"organization": {"org-1", "org-2"},
		}}
		conditions := ApplyConditions(scope, "", dimCols("organization", "organization_id"))
		assert.Len(t, conditions, 1)
	})

	t.Run("two matching dimensions", func(t *testing.T) {
		scope := &security.DataScope{Dimensions: map[string][]string{
			"organization": {"org-1"},
			"counterparty": {"cp-1", "cp-2"},
		}}
		conditions := ApplyConditions(scope, "", dimCols(
			"organization", "organization_id",
			"counterparty", "counterparty_id",
		))
		assert.Len(t, conditions, 2)
	})

	t.Run("dimension not in scope — no condition for it", func(t *testing.T) {
		scope := &security.DataScope{Dimensions: map[string][]string{
			"organization": {"org-1"},
		}}
		// Entity has counterparty column but user has no counterparty restriction
		conditions := ApplyConditions(scope, "", dimCols(
			"organization", "organization_id",
			"counterparty", "counterparty_id",
		))
		assert.Len(t, conditions, 1) // only org
	})

	t.Run("empty dimColumns — no conditions", func(t *testing.T) {
		scope := &security.DataScope{Dimensions: map[string][]string{
			"organization": {"org-1"},
		}}
		conditions := ApplyConditions(scope, "", map[string]string{})
		assert.Len(t, conditions, 0)
	})

	t.Run("empty allowed IDs — impossible condition", func(t *testing.T) {
		scope := &security.DataScope{Dimensions: map[string][]string{
			"organization": {}, // present but empty = deny all
		}}
		conditions := ApplyConditions(scope, "", dimCols("organization", "organization_id"))
		assert.Len(t, conditions, 1) // impossible WHERE condition
	})
}

func TestEmptyDimensionMatchesNoRowsIncludingNull(t *testing.T) {
	scope := &security.DataScope{Dimensions: map[string][]string{"organization": {}}}
	conditions := ApplyConditions(scope, "catalog", map[string]string{"organization": "organization_id"})
	if len(conditions) != 1 {
		t.Fatalf("expected one denying condition: %v", conditions)
	}
	sql, args, err := conditions[0].ToSql()
	if err != nil || sql != "FALSE" || len(args) != 0 {
		t.Fatalf("expected FALSE without arguments: %q %v %v", sql, args, err)
	}
	if scope.CanAccessRecord("catalog", map[string]string{"organization": ""}) {
		t.Fatal("NULL dimension cannot pass an empty allowed set")
	}
}
