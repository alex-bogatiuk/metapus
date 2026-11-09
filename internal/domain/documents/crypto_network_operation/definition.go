package crypto_network_operation

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "crypto_network_operation",
	Name:  DocumentType,
	Route: "crypto-network-operation",
	Table: "doc_crypto_network_operations",
	Kind:  entity.DocumentKind,
}

func (CryptoNetworkOperation) EntityDefinition() entity.Definition { return Definition }
