package document_repo

import (
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/infrastructure/storage/postgres"
)

type CryptoCustodyReceiptRepo struct {
	*BaseDocumentRepo[*crypto_custody_receipt.CryptoCustodyReceipt]
}

func NewCryptoCustodyReceiptRepo() *CryptoCustodyReceiptRepo {
	return &CryptoCustodyReceiptRepo{BaseDocumentRepo: NewDocumentRepoForDefinition(
		crypto_custody_receipt.Definition,
		postgres.ExtractDBColumns[crypto_custody_receipt.CryptoCustodyReceipt](),
		func() *crypto_custody_receipt.CryptoCustodyReceipt {
			return &crypto_custody_receipt.CryptoCustodyReceipt{}
		},
	)}
}
