package merchant

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "merchant",
	Name:  "Merchant",
	Route: "merchants",
	Table: "cat_merchants",
	Kind:  entity.CatalogKind,
}

func (Merchant) EntityDefinition() entity.Definition { return Definition }
