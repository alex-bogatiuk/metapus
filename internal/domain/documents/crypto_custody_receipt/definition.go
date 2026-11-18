package crypto_custody_receipt

import "metapus/internal/core/entity"

const DocumentType = "CryptoCustodyReceipt"

var Definition = entity.Definition{
	Key: "crypto_custody_receipt", Name: DocumentType,
	Route: "crypto-custody-receipt", Table: "doc_crypto_custody_receipts", Kind: entity.DocumentKind,
	Number: entity.NumberRule{Prefix: "CCR"},
}

func (CryptoCustodyReceipt) EntityDefinition() entity.Definition { return Definition }
