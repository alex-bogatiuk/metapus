package crypto_invoice

import "github.com/alex-bogatiuk/metapus/pkg/sdk/entity"

var Definition = entity.Definition{
	Key:           "crypto_invoice",
	Name:          DocumentType,
	Route:         "crypto-invoice",
	Table:         "doc_crypto_invoices",
	Kind:          entity.DocumentKind,
	Number:        entity.NumberRule{Prefix: "CI"},
	RLSDimensions: map[string]string{"merchant": "merchant_id"},
}

func (CryptoInvoice) EntityDefinition() entity.Definition { return Definition }
