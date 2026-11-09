package dto

import (
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/domain/documents/crypto_resource_rental"
)

// These documents use their JSON entity projection in the generic HTTP presenter.
type CryptoNetworkOperationResponse = crypto_network_operation.CryptoNetworkOperation
type CryptoProviderBalanceOperationResponse = crypto_provider_balance_operation.CryptoProviderBalanceOperation
type CryptoResourceRentalResponse = crypto_resource_rental.CryptoResourceRental

// TypedCursorListResponse describes the concrete item projection selected by a factory.
type TypedCursorListResponse[T any] struct {
	Items       []T    `json:"items"`
	NextCursor  string `json:"nextCursor,omitempty"`
	PrevCursor  string `json:"prevCursor,omitempty"`
	HasMore     bool   `json:"hasMore"`
	HasPrev     bool   `json:"hasPrev"`
	TargetIndex *int   `json:"targetIndex,omitempty"`
	TotalCount  *int64 `json:"totalCount"`
}
