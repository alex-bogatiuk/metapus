package document_repo

import (
	"metapus/internal/domain/documents/crypto_resource_rental"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoResourceRentalRepo persists system-generated reusable resource leases.
type CryptoResourceRentalRepo struct {
	*BaseDocumentRepo[*crypto_resource_rental.CryptoResourceRental]
}

func NewCryptoResourceRentalRepo() *CryptoResourceRentalRepo {
	return &CryptoResourceRentalRepo{BaseDocumentRepo: NewDocumentRepoForDefinition[*crypto_resource_rental.CryptoResourceRental](crypto_resource_rental.Definition,
		postgres.ExtractDBColumns[crypto_resource_rental.CryptoResourceRental](),
		func() *crypto_resource_rental.CryptoResourceRental {
			return &crypto_resource_rental.CryptoResourceRental{}
		},
	)}
}
