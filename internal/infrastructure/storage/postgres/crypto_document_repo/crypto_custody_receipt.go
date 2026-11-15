package crypto_document_repo

import (
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
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
