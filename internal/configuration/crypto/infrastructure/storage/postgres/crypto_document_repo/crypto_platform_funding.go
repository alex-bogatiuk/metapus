package crypto_document_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_platform_funding"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
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
