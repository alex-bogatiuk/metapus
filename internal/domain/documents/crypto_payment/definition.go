package crypto_payment

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "crypto_payment",
	Name:  "CryptoPayment",
	Route: "crypto-payment",
	Table: "doc_crypto_payments",
	Kind:  entity.DocumentKind,
}

func (CryptoPayment) EntityDefinition() entity.Definition { return Definition }
