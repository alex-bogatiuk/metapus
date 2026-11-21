package crypto_document_repo

import (
	"metapus/internal/configuration/crypto/domain/documents/crypto_platform_funding"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

type CryptoPlatformFundingRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_platform_funding.CryptoPlatformFunding]
}

func NewCryptoPlatformFundingRepo() *CryptoPlatformFundingRepo {
	return &CryptoPlatformFundingRepo{BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition(
		crypto_platform_funding.Definition,
		postgres.ExtractDBColumns[crypto_platform_funding.CryptoPlatformFunding](),
		func() *crypto_platform_funding.CryptoPlatformFunding {
			return &crypto_platform_funding.CryptoPlatformFunding{}
		},
	)}
}
