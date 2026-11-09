package currency

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "currency",
	Name:  "Currency",
	Route: "currencies",
	Table: "cat_currencies",
	Kind:  entity.CatalogKind,
}

func (Currency) EntityDefinition() entity.Definition { return Definition }
