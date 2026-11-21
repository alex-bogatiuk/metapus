package token

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "token",
	Name:  "Token",
	Route: "tokens",
	Table: "cat_tokens",
	Kind:  entity.CatalogKind,
	// The service takes the code from the token symbol.
	Code: entity.CodeRule{Source: entity.CodeDerived},
}

func (Token) EntityDefinition() entity.Definition { return Definition }
