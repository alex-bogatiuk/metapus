package crypto_document_repo

import (
	"metapus/internal/configuration/crypto/domain/documents/crypto_resource_rental"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/postgres/document_repo"
)

// CryptoResourceRentalRepo persists system-generated reusable resource leases.
type CryptoResourceRentalRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_resource_rental.CryptoResourceRental]
}

func NewCryptoResourceRentalRepo() *CryptoResourceRentalRepo {
	return &CryptoResourceRentalRepo{BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition[*crypto_resource_rental.CryptoResourceRental](crypto_resource_rental.Definition,
		postgres.ExtractDBColumns[crypto_resource_rental.CryptoResourceRental](),
		func() *crypto_resource_rental.CryptoResourceRental {
			return &crypto_resource_rental.CryptoResourceRental{}
		},
	)}
}
