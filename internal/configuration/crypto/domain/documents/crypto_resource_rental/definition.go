package crypto_resource_rental

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:    "crypto_resource_rental",
	Name:   "CryptoResourceRental",
	Route:  "crypto-resource-rental",
	Table:  "doc_crypto_resource_rentals",
	Kind:   entity.DocumentKind,
	Number: entity.NumberRule{Prefix: "RR"},
}

func (CryptoResourceRental) EntityDefinition() entity.Definition { return Definition }
