package wallet

import (
	"net/http"
	"testing"
)

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

func TestNewWalletPoolExhausted(t *testing.T) {
	const networkID = "network-123"

	err := NewWalletPoolExhausted(networkID, 0)

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
	if got := NewWalletPoolExhausted(networkID, 45).Details["retryAfterSeconds"]; got != 45 {
		t.Fatalf("explicit retryAfterSeconds = %v, want 45", got)
	}
}
