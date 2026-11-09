package dto

import (
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_network_operation"
)

type CreateCryptoNetworkOperationRequest struct{}

func (CreateCryptoNetworkOperationRequest) ToEntity() *crypto_network_operation.CryptoNetworkOperation {
	return &crypto_network_operation.CryptoNetworkOperation{}
}

type UpdateCryptoNetworkOperationRequest struct {
	SourceWalletID id.ID              `json:"sourceWalletId" binding:"required"`
	CostTokenID    id.ID              `json:"costTokenId" binding:"required"`
	CostAmount     types.CryptoAmount `json:"costAmount"`
	Description    string             `json:"description"`
	Version        int                `json:"version" binding:"required"`
}

func (r UpdateCryptoNetworkOperationRequest) ApplyTo(doc *crypto_network_operation.CryptoNetworkOperation) {
	doc.SourceWalletID = r.SourceWalletID
	doc.CostTokenID = r.CostTokenID
	doc.CostAmount = r.CostAmount
	doc.Description = r.Description
	doc.Version = r.Version
}

func FromCryptoNetworkOperation(doc *crypto_network_operation.CryptoNetworkOperation) any { return doc }
