package dto

import "metapus/internal/domain/documents/crypto_resource_rental"

// CryptoResourceRental is system-managed; mutation DTOs only satisfy the generic handler contract.
type CreateCryptoResourceRentalRequest struct{}

func (CreateCryptoResourceRentalRequest) ToEntity() *crypto_resource_rental.CryptoResourceRental {
	return &crypto_resource_rental.CryptoResourceRental{}
}

type UpdateCryptoResourceRentalRequest struct {
	Version int `json:"version"`
}

func (r UpdateCryptoResourceRentalRequest) ApplyTo(doc *crypto_resource_rental.CryptoResourceRental) {
	doc.Version = r.Version
}

func FromCryptoResourceRental(doc *crypto_resource_rental.CryptoResourceRental) any { return doc }
