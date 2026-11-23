package crypto_provider_account

import "github.com/alex-bogatiuk/metapus/pkg/sdk/entity"

var Definition = entity.Definition{
	Key:   "crypto_provider_account",
	Name:  "CryptoProviderAccount",
	Route: "crypto-provider-accounts",
	Table: "cat_crypto_provider_accounts",
	Kind:  entity.CatalogKind,
}

func (CryptoProviderAccount) EntityDefinition() entity.Definition { return Definition }
