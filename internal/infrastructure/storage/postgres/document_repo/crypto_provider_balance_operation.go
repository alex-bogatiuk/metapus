package document_repo

import (
	"metapus/internal/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/infrastructure/storage/postgres"
)

type CryptoProviderBalanceOperationRepo struct {
	*BaseDocumentRepo[*crypto_provider_balance_operation.CryptoProviderBalanceOperation]
}

func NewCryptoProviderBalanceOperationRepo() *CryptoProviderBalanceOperationRepo {
	return &CryptoProviderBalanceOperationRepo{BaseDocumentRepo: NewDocumentRepoForDefinition[*crypto_provider_balance_operation.CryptoProviderBalanceOperation](crypto_provider_balance_operation.Definition,
		postgres.ExtractDBColumns[crypto_provider_balance_operation.CryptoProviderBalanceOperation](),
		func() *crypto_provider_balance_operation.CryptoProviderBalanceOperation {
			return &crypto_provider_balance_operation.CryptoProviderBalanceOperation{}
		},
	)}
}
