package crypto_withdrawal

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "crypto_withdrawal",
	Name:  DocumentType,
	Route: "crypto-withdrawal",
	Table: "doc_crypto_withdrawals",
	Kind:  entity.DocumentKind,
}

func (CryptoWithdrawal) EntityDefinition() entity.Definition { return Definition }
