package crypto_invoice

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "crypto_invoice",
	Name:  DocumentType,
	Route: "crypto-invoice",
	Table: "doc_crypto_invoices",
	Kind:  entity.DocumentKind,
}

func (CryptoInvoice) EntityDefinition() entity.Definition { return Definition }
