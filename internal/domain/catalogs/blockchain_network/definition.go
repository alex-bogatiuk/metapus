package blockchain_network

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "blockchain_network",
	Name:  "BlockchainNetwork",
	Route: "blockchain-networks",
	Table: "cat_blockchain_networks",
	Kind:  entity.CatalogKind,
	// The service builds the code from the chain family and network identifier.
	Code: entity.CodeRule{Source: entity.CodeDerived},
}

func (BlockchainNetwork) EntityDefinition() entity.Definition { return Definition }
