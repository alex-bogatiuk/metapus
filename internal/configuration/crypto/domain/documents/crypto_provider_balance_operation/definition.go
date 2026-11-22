package crypto_provider_balance_operation

import "metapus/pkg/sdk/entity"

var Definition = entity.Definition{
	Key:    "crypto_provider_balance_operation",
	Name:   "CryptoProviderBalanceOperation",
	Route:  "crypto-provider-balance-operation",
	Table:  "doc_crypto_provider_balance_operations",
	Kind:   entity.DocumentKind,
	Number: entity.NumberRule{Prefix: "PBO"},
}

func (CryptoProviderBalanceOperation) EntityDefinition() entity.Definition { return Definition }
