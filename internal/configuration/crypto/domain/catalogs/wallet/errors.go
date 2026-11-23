package wallet

import (
	"net/http"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
)

// Wallet pool and merchant address admission error codes, part of the public
// merchant API.
const (
	CodeWalletPoolExhausted                    = "WALLET_POOL_EXHAUSTED"
	CodeMerchantWalletLeaseLimitExceeded       = "MERCHANT_WALLET_LEASE_LIMIT_EXCEEDED"
	CodeMerchantPersistentAddressLimitExceeded = "MERCHANT_PERSISTENT_ADDRESS_LIMIT_EXCEEDED"
)

// Wallet pool unavailability reasons. They extend WALLET_POOL_EXHAUSTED
// details so operators can tell a disabled or blocked pool from an empty one;
// the public code and retry contract stay the same for merchants.
const (
	PoolReasonNoReadyWallet   = "no_ready_wallet"
	PoolReasonDisabled        = "pool_disabled"
	PoolReasonNeedsReview     = "pool_needs_review"
	PoolReasonWatcherLag      = "watcher_lag"
	PoolReasonNetworkDisabled = "network_disabled"
	PoolReasonNetworkHold     = "network_hold"
)

// NewWalletPoolUnavailable is WALLET_POOL_EXHAUSTED with the operational reason.
func NewWalletPoolUnavailable(networkID any, retryAfter int, reason string) *apperror.AppError {
	err := NewWalletPoolExhausted(networkID, retryAfter)
	err.Details["reason"] = reason
	return err
}

// NewWalletPoolExhausted creates a retryable capacity error for exhausted
// receiving wallet pools. A non-positive retryAfter defaults to 30 seconds.
func NewWalletPoolExhausted(networkID any, retryAfter int) *apperror.AppError {
	if retryAfter <= 0 {
		retryAfter = 30
	}
	return &apperror.AppError{
		Code:       CodeWalletPoolExhausted,
		Message:    "Нет свободного кошелька для приема платежа. Повторите позже или обратитесь в поддержку.",
		HTTPStatus: http.StatusServiceUnavailable,
		Details: map[string]any{
			"resource":          "wallet_pool",
			"networkId":         networkID,
			"retryAfterSeconds": retryAfter,
		},
	}
}

// NewMerchantWalletLeaseLimitExceeded creates a retryable merchant-scoped
// admission error. It intentionally omits quota counters from public details.
func NewMerchantWalletLeaseLimitExceeded(networkID any, retryAfter int) *apperror.AppError {
	if retryAfter <= 0 {
		retryAfter = 30
	}
	return &apperror.AppError{
		Code:       CodeMerchantWalletLeaseLimitExceeded,
		Message:    "Лимит новых платежных адресов временно исчерпан. Повторите позже.",
		HTTPStatus: http.StatusTooManyRequests,
		Details: map[string]any{
			"resource":          "wallet_lease",
			"networkId":         networkID,
			"retryAfterSeconds": retryAfter,
		},
	}
}

// NewMerchantPersistentAddressLimitExceeded reports a durable merchant quota.
func NewMerchantPersistentAddressLimitExceeded(networkID any) *apperror.AppError {
	return &apperror.AppError{
		Code:       CodeMerchantPersistentAddressLimitExceeded,
		Message:    "Достигнут лимит постоянных платежных адресов. Обратитесь в поддержку.",
		HTTPStatus: http.StatusUnprocessableEntity,
		Details: map[string]any{
			"resource":  "persistent_wallet",
			"networkId": networkID,
		},
	}
}
