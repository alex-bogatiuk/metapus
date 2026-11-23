package crypto_network_operation

import "github.com/alex-bogatiuk/metapus/pkg/sdk/entity"

var Definition = entity.Definition{
	Key:    "crypto_network_operation",
	Name:   DocumentType,
	Route:  "crypto-network-operation",
	Table:  "doc_crypto_network_operations",
	Kind:   entity.DocumentKind,
	Number: entity.NumberRule{Prefix: "CNO"},
}

func (CryptoNetworkOperation) EntityDefinition() entity.Definition { return Definition }
