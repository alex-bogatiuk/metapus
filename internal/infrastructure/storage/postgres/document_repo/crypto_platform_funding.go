package document_repo

import (
	"metapus/internal/domain/documents/crypto_platform_funding"
	"metapus/internal/infrastructure/storage/postgres"
)

type CryptoPlatformFundingRepo struct {
	*BaseDocumentRepo[*crypto_platform_funding.CryptoPlatformFunding]
}

func NewCryptoPlatformFundingRepo() *CryptoPlatformFundingRepo {
	return &CryptoPlatformFundingRepo{BaseDocumentRepo: NewDocumentRepoForDefinition(
		crypto_platform_funding.Definition,
		postgres.ExtractDBColumns[crypto_platform_funding.CryptoPlatformFunding](),
		func() *crypto_platform_funding.CryptoPlatformFunding {
			return &crypto_platform_funding.CryptoPlatformFunding{}
		},
	)}
}
