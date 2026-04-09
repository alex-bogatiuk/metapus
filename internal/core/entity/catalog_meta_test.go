package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCatalogMeta_HierarchicalCatalogs(t *testing.T) {
	hierarchical := []string{"nomenclature", "counterparty", "warehouse"}
	for _, name := range hierarchical {
		meta := GetCatalogMeta(name)
		assert.True(t, meta.Hierarchical, "expected %s to be hierarchical", name)
		assert.Equal(t, HierarchyGroupsAndItems, meta.HierarchyType,
			"expected %s to use GroupsAndItems hierarchy", name)
		assert.True(t, meta.FolderAsParentOnly,
			"expected %s to require folder as parent", name)
	}
}

func TestGetCatalogMeta_FlatCatalogs(t *testing.T) {
	flat := []string{"organization", "currency", "unit", "vat_rate", "contract"}
	for _, name := range flat {
		meta := GetCatalogMeta(name)
		assert.False(t, meta.Hierarchical, "expected %s to be flat", name)
	}
}

func TestGetCatalogMeta_UnknownReturnsFlat(t *testing.T) {
	meta := GetCatalogMeta("nonexistent_catalog")
	assert.False(t, meta.Hierarchical, "unknown catalog should default to flat")
}

func TestRegisterCatalogMeta(t *testing.T) {
	RegisterCatalogMeta("test_catalog", CatalogMeta{
		Hierarchical:  true,
		HierarchyType: HierarchyItemsOnly,
		MaxDepth:      3,
	})

	meta := GetCatalogMeta("test_catalog")
	assert.True(t, meta.Hierarchical)
	assert.Equal(t, HierarchyItemsOnly, meta.HierarchyType)
	assert.Equal(t, 3, meta.MaxDepth)

	// Cleanup
	delete(catalogRegistry, "test_catalog")
}
