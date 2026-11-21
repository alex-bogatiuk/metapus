package blockchain_network

import (
	"net/http"

	"metapus/internal/core/apperror"
)

// CodeNetworkUnavailable is part of the public merchant API.
const CodeNetworkUnavailable = "CRYPTO_NETWORK_UNAVAILABLE"

// NewNetworkUnavailable rejects new payment allocations while a network
// policy is disabled or an operational hold is open. Existing events and
// delivery jobs remain durable and can resume after the hold is cleared.
func NewNetworkUnavailable(networkID any) *apperror.AppError {
	return &apperror.AppError{
		Code:       CodeNetworkUnavailable,
		Message:    "Платёжная сеть временно недоступна. Повторите позже.",
		HTTPStatus: http.StatusServiceUnavailable,
		Details: map[string]any{
			"resource":          "crypto_network",
			"networkId":         networkID,
			"retryAfterSeconds": 60,
		},
	}
}
