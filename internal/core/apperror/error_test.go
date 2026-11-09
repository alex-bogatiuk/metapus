package apperror

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

func TestNewWalletPoolExhausted(t *testing.T) {
	const networkID = "network-123"

	err := NewWalletPoolExhausted(networkID)

	if err.Code != CodeWalletPoolExhausted {
		t.Fatalf("Code = %q, want %q", err.Code, CodeWalletPoolExhausted)
	}
	if err.Message != "Нет свободного кошелька для приема платежа. Повторите позже или обратитесь в поддержку." {
		t.Fatalf("Message = %q", err.Message)
	}
	if err.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("HTTPStatus = %d, want %d", err.HTTPStatus, http.StatusServiceUnavailable)
	}
	if err.Details["resource"] != "wallet_pool" {
		t.Fatalf("Details[resource] = %v, want wallet_pool", err.Details["resource"])
	}
	if err.Details["networkId"] != networkID {
		t.Fatalf("Details[networkId] = %v, want %s", err.Details["networkId"], networkID)
	}
	if err.Details["retryAfterSeconds"] != 30 {
		t.Fatalf("Details[retryAfterSeconds] = %v, want 30", err.Details["retryAfterSeconds"])
	}
}

func TestNewMerchantWalletLeaseLimitExceeded(t *testing.T) {
	err := NewMerchantWalletLeaseLimitExceeded("network-123", 17)
	if err.Code != CodeMerchantWalletLeaseLimitExceeded {
		t.Fatalf("Code = %q", err.Code)
	}
	if err.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("HTTPStatus = %d, want %d", err.HTTPStatus, http.StatusTooManyRequests)
	}
	if err.Details["retryAfterSeconds"] != 17 {
		t.Fatalf("retryAfterSeconds = %v", err.Details["retryAfterSeconds"])
	}
}

func TestNewCryptoNetworkUnavailable(t *testing.T) {
	err := NewCryptoNetworkUnavailable("network-123")
	require.Equal(t, CodeCryptoNetworkUnavailable, err.Code)
	require.Equal(t, http.StatusServiceUnavailable, err.HTTPStatus)
	require.Equal(t, "crypto_network", err.Details["resource"])
	require.Equal(t, "network-123", err.Details["networkId"])
	require.Equal(t, 60, err.Details["retryAfterSeconds"])
}

func TestAppErrorPreservesFixedPointQuantities(t *testing.T) {
	requested := types.NewQuantityFromInt64Scaled(9_007_199_254_740_993)
	available := types.NewQuantityFromInt64Scaled(9_007_199_254_740_991)

	appErr := NewBusinessRule("INSUFFICIENT_QUANTITY", "Недостаточное количество").
		WithDetail("requested", requested).
		WithDetail("available", available)

	encoded, err := json.Marshal(appErr)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"requested":900719925474.0993`)
	require.Contains(t, string(encoded), `"available":900719925474.0991`)
	require.JSONEq(t, `{
		"code":"INSUFFICIENT_QUANTITY",
		"message":"Недостаточное количество",
		"details":{
			"requested":900719925474.0993,
			"available":900719925474.0991
		}
	}`, string(encoded))
}
