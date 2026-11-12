package crypto_platform_funding

import "metapus/internal/core/entity"

const DocumentType = "CryptoPlatformFunding"

var Definition = entity.Definition{
	Key: "crypto_platform_funding", Name: DocumentType,
	Route: "crypto-platform-funding", Table: "doc_crypto_platform_fundings", Kind: entity.DocumentKind,
}

func (CryptoPlatformFunding) EntityDefinition() entity.Definition { return Definition }
