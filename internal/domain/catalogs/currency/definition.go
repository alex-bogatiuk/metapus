package currency

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "currency",
	Name:  "Currency",
	Route: "currencies",
	Table: "cat_currencies",
	Kind:  entity.CatalogKind,
	// The service takes the code from the ISO code or ticker.
	Code: entity.CodeRule{Source: entity.CodeDerived},
}

func (Currency) EntityDefinition() entity.Definition { return Definition }
