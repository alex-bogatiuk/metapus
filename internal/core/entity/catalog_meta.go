package entity

// HierarchyType defines how hierarchy works for a catalog.
// Inspired by 1C:Enterprise hierarchy modes.
type HierarchyType string

const (
	// HierarchyGroupsAndItems — groups (folders) and items coexist.
	// Items can only be children of groups, not other items.
	// Analogy: 1С — "ГруппыИЭлементы".
	HierarchyGroupsAndItems HierarchyType = "groups_and_items"

	// HierarchyItemsOnly — all elements can be parents.
	// No distinction between folders and items.
	// Analogy: 1С — "Элементы".
	HierarchyItemsOnly HierarchyType = "items_only"
)

// CatalogMeta defines metadata configuration for a catalog type.
// Controls whether hierarchy is supported and how it behaves.
// This is the metadata-driven approach — behavior is configured, not hardcoded.
type CatalogMeta struct {
	// Hierarchical indicates whether this catalog supports parent-child relationships.
	// If false, ParentID and IsFolder fields are ignored in business logic.
	Hierarchical bool

	// HierarchyType defines the hierarchy mode (only relevant if Hierarchical is true).
	HierarchyType HierarchyType

	// MaxDepth limits the nesting depth (0 = unlimited).
	// Only relevant if Hierarchical is true.
	MaxDepth int

	// FolderAsParentOnly requires that parent must be a folder (IsFolder=true).
	// Only relevant if HierarchyType is HierarchyGroupsAndItems.
	FolderAsParentOnly bool
}

// catalogRegistry stores metadata for all known catalog types.
// Key is the entity name (lowercase, matches CatalogService.entityName).
var catalogRegistry = map[string]CatalogMeta{
	// Hierarchical catalogs (groups and items)
	"nomenclature": {
		Hierarchical:       true,
		HierarchyType:      HierarchyGroupsAndItems,
		FolderAsParentOnly: true,
	},
	"counterparty": {
		Hierarchical:       true,
		HierarchyType:      HierarchyGroupsAndItems,
		FolderAsParentOnly: true,
	},
	"warehouse": {
		Hierarchical:       true,
		HierarchyType:      HierarchyGroupsAndItems,
		FolderAsParentOnly: true,
	},

	// Flat catalogs (no hierarchy)
	"organization": {Hierarchical: false},
	"currency":     {Hierarchical: false},
	"unit":         {Hierarchical: false},
	"vat_rate":     {Hierarchical: false},
	"contract":     {Hierarchical: false},
}

// GetCatalogMeta returns metadata for a catalog type by entity name.
// Returns a non-hierarchical default if the entity name is not registered.
func GetCatalogMeta(entityName string) CatalogMeta {
	if meta, ok := catalogRegistry[entityName]; ok {
		return meta
	}
	// Default: flat catalog (safe fallback)
	return CatalogMeta{Hierarchical: false}
}

// RegisterCatalogMeta registers or overrides metadata for a catalog type.
// Use for extending the system with new catalog types without modifying this file.
func RegisterCatalogMeta(entityName string, meta CatalogMeta) {
	catalogRegistry[entityName] = meta
}
