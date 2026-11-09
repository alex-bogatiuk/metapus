package entity

// HierarchyType defines how hierarchy works for a catalog.
// Inspired by 1C:Enterprise hierarchy modes.
type HierarchyType string

const (
	// HierarchyGroupsAndItems — groups (folders) and items coexist.
	// Items can only be children of groups, not other items.
	// Analogy: 1C — "GroupsAndItems".
	HierarchyGroupsAndItems HierarchyType = "groups_and_items"

	// HierarchyItemsOnly — all elements can be parents.
	// No distinction between folders and items.
	// Analogy: 1C — "Items".
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
