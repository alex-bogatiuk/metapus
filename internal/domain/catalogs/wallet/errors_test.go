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
