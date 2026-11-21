package blockchain_network

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewNetworkUnavailable(t *testing.T) {
	err := NewNetworkUnavailable("network-123")
	require.Equal(t, CodeNetworkUnavailable, err.Code)
	require.Equal(t, http.StatusServiceUnavailable, err.HTTPStatus)
	require.Equal(t, "crypto_network", err.Details["resource"])
	require.Equal(t, "network-123", err.Details["networkId"])
	require.Equal(t, 60, err.Details["retryAfterSeconds"])
}
