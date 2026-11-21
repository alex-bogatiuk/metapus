package wallet

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Merchants keep the retryable public code; operators get the real reason.
func TestWalletPoolUnavailableKeepsPublicCodeAndAddsReason(t *testing.T) {
	err := NewWalletPoolUnavailable("net-1", 0, PoolReasonDisabled)
	require.Equal(t, CodeWalletPoolExhausted, err.Code)
	require.Equal(t, http.StatusServiceUnavailable, err.HTTPStatus)
	require.Equal(t, PoolReasonDisabled, err.Details["reason"])
	require.Equal(t, 30, err.Details["retryAfterSeconds"])
}
