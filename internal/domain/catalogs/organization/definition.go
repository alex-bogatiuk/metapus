package organization

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "organization",
	Name:  "Organization",
	Route: "organizations",
	Table: "cat_organizations",
	Kind:  entity.CatalogKind,
}

func (Organization) EntityDefinition() entity.Definition { return Definition }
