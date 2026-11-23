package crypto_document_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_custody_receipt"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
)

type CryptoCustodyReceiptRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_custody_receipt.CryptoCustodyReceipt]
}

func NewCryptoCustodyReceiptRepo() *CryptoCustodyReceiptRepo {
	return &CryptoCustodyReceiptRepo{BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition(
		crypto_custody_receipt.Definition,
		postgres.ExtractDBColumns[crypto_custody_receipt.CryptoCustodyReceipt](),
		func() *crypto_custody_receipt.CryptoCustodyReceipt {
			return &crypto_custody_receipt.CryptoCustodyReceipt{}
		},
	)}
}
