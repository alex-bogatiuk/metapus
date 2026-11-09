package crypto_provider_account

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "crypto_provider_account",
	Name:  "CryptoProviderAccount",
	Route: "crypto-provider-accounts",
	Table: "cat_crypto_provider_accounts",
	Kind:  entity.CatalogKind,
}

func (CryptoProviderAccount) EntityDefinition() entity.Definition { return Definition }
