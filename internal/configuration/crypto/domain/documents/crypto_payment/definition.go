package crypto_payment

import "metapus/pkg/sdk/entity"

var Definition = entity.Definition{
	Key:    "crypto_payment",
	Name:   "CryptoPayment",
	Route:  "crypto-payment",
	Table:  "doc_crypto_payments",
	Kind:   entity.DocumentKind,
	Number: entity.NumberRule{Prefix: "CP"},
}

func (CryptoPayment) EntityDefinition() entity.Definition { return Definition }
