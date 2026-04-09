package metadata

import (
	"metapus/internal/infrastructure/cache"
	"testing"
)

func TestEntityDef_MergeCustomFields(t *testing.T) {
	// 1. Create dummy schema cache or mock it
	sc := cache.NewSchemaCache(nil) // It will have no fields without DB, but let's check its structure

	def := EntityDef{
		Name: "Counterparty",
		Key:  "counterparty",
		Fields: []FieldDef{
			{Name: "name", Label: "Name", Type: TypeString},
		},
	}

	// Because SchemaCache uses internal map, I'll need to manually add something if possible
	// or create a test-only way.
	// Looking at schema_cache.go, it only populates from DB.
	// Let's assume for a unit test we want to ensure it handles nil cache or no fields gracefully.

	def.MergeCustomFields(nil)
	if len(def.CustomFields) != 0 {
		t.Errorf("Expected 0 custom fields, got %d", len(def.CustomFields))
	}

	def.MergeCustomFields(sc)
	if len(def.CustomFields) != 0 {
		t.Errorf("Expected 0 custom (initial) fields, got %d", len(def.CustomFields))
	}

	// Let's verify ToFilterMeta includes CustomFields if they exist.
	def.CustomFields = []FieldDef{
		{Name: "attributes.inn", Label: "INN", Type: TypeString},
	}

	filters := def.ToFilterMeta(nil)
	foundInn := false
	for _, f := range filters {
		if f.Key == "attributes.inn" {
			foundInn = true
			if f.Group != "Additional attributes" {
				t.Errorf("Expected group 'Additional attributes', got %s", f.Group)
			}
		}
	}

	if !foundInn {
		t.Errorf("Custom field 'attributes.inn' not found in filter meta")
	}
}
