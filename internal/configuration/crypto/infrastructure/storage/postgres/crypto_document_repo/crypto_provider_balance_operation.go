package crypto_document_repo

import (
	"metapus/internal/configuration/crypto/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

type CryptoProviderBalanceOperationRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_provider_balance_operation.CryptoProviderBalanceOperation]
}

func NewCryptoProviderBalanceOperationRepo() *CryptoProviderBalanceOperationRepo {
	return &CryptoProviderBalanceOperationRepo{BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition[*crypto_provider_balance_operation.CryptoProviderBalanceOperation](crypto_provider_balance_operation.Definition,
		postgres.ExtractDBColumns[crypto_provider_balance_operation.CryptoProviderBalanceOperation](),
		func() *crypto_provider_balance_operation.CryptoProviderBalanceOperation {
			return &crypto_provider_balance_operation.CryptoProviderBalanceOperation{}
		},
	)}
}
