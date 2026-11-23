package crypto_merchant_credit

import "github.com/alex-bogatiuk/metapus/pkg/sdk/entity"

const DocumentType = "CryptoMerchantCredit"

var Definition = entity.Definition{
	Key: "crypto_merchant_credit", Name: DocumentType,
	Route: "crypto-merchant-credit", Table: "doc_crypto_merchant_credits", Kind: entity.DocumentKind,
	Number: entity.NumberRule{Prefix: "CMC"},
}

func (CryptoMerchantCredit) EntityDefinition() entity.Definition { return Definition }
