package contract

import "github.com/alex-bogatiuk/metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "contract",
	Name:  "Contract",
	Route: "contracts",
	Table: "cat_contracts",
	Kind:  entity.CatalogKind,
	Code:  entity.CodeRule{Prefix: "CT"},
}

func (Contract) EntityDefinition() entity.Definition { return Definition }
