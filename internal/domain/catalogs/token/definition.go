package token

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "token",
	Name:  "Token",
	Route: "tokens",
	Table: "cat_tokens",
	Kind:  entity.CatalogKind,
}

func (Token) EntityDefinition() entity.Definition { return Definition }
