package tree_probe

import "metapus/internal/core/entity"

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "tree_probe", Name: "TreeProbe", Route: "tree-probes", Table: "cat_tree_probes",
	Kind:      entity.CatalogKind,
	Hierarchy: entity.CatalogMeta{Hierarchical: true, HierarchyType: entity.HierarchyGroupsAndItems, FolderAsParentOnly: true},
}
