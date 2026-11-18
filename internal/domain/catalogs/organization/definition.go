package organization

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "organization",
	Name:  "Organization",
	Route: "organizations",
	Table: "cat_organizations",
	Kind:  entity.CatalogKind,
	Code:  entity.CodeRule{Prefix: "ORG"},
}

func (Organization) EntityDefinition() entity.Definition { return Definition }
