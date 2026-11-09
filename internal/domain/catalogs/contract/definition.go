package contract

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "contract",
	Name:  "Contract",
	Route: "contracts",
	Table: "cat_contracts",
	Kind:  entity.CatalogKind,
}

func (Contract) EntityDefinition() entity.Definition { return Definition }
