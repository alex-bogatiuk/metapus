package wallet

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "wallet",
	Name:  "Wallet",
	Route: "wallets",
	Table: "cat_wallets",
	Kind:  entity.CatalogKind,
}

func (Wallet) EntityDefinition() entity.Definition { return Definition }
