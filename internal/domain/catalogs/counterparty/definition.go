package counterparty

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:       "counterparty",
	Name:      "Counterparty",
	Route:     "counterparties",
	Table:     "cat_counterparties",
	Kind:      entity.CatalogKind,
	Hierarchy: entity.CatalogMeta{Hierarchical: true, HierarchyType: entity.HierarchyGroupsAndItems, FolderAsParentOnly: true},
}

func (Counterparty) EntityDefinition() entity.Definition { return Definition }
