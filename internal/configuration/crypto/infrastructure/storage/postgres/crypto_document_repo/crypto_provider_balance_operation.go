package crypto_document_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_provider_balance_operation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
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
