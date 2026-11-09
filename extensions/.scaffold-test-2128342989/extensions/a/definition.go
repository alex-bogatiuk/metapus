package a

import "metapus/internal/core/entity"

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "a", Name: "A", Route: "as", Table: "cat_as",
	Kind: entity.CatalogKind,
}
