// internal/domain/crypto/webhook_test.go
package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		give    string
		url     string
		wantErr bool
	}{
		// Valid
		{"valid HTTPS URL", "https://merchant.example.com/webhook", false},
		{"valid HTTPS with path and port", "https://api.merchant.com:8443/v1/webhooks/crypto", false},
		{"empty URL (no webhook configured)", "", false},

		// Protocol violations
		{"HTTP not allowed", "http://merchant.example.com/webhook", true},
		{"FTP not allowed", "ftp://merchant.example.com/webhook", true},

		// Loopback / private IPs
		{"loopback IPv4", "https://127.0.0.1/webhook", true},
		{"loopback IPv6", "https://[::1]/webhook", true},
		{"private 10.x", "https://10.0.0.1/webhook", true},
		{"private 172.16.x", "https://172.16.0.1/webhook", true},
		{"private 192.168.x", "https://192.168.1.1/webhook", true},

		// Cloud metadata SSRF
		{"AWS metadata IPv4", "https://169.254.169.254/latest/meta-data/", true},

		// Internal hostnames
		{"localhost hostname", "https://localhost/webhook", true},
		{"*.internal suffix", "https://metadata.google.internal/webhook", true},
		{"generic .internal", "https://some-service.internal/webhook", true},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			err := ValidateWebhookURL(tt.url)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateWebhookURL(%q) = nil, want error", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateWebhookURL(%q) = %v, want nil", tt.url, err)
			}
		})
	}
}

func TestWebhookSign_IncludesTimestamp(t *testing.T) {
	d := NewWebhookDispatcher()
	payload := []byte(`{"event":"invoice.paid","data":{}}`)
	secret := "merchant-webhook-secret-key"

	sig1 := d.sign(payload, secret, "2026-05-06T12:00:00Z")
	sig2 := d.sign(payload, secret, "2026-05-06T12:01:00Z")

	// Same payload + different timestamp → different HMAC (replay-resistant)
	if sig1 == sig2 {
		t.Error("sign() should produce different signatures for different timestamps (replay protection)")
	}

	// Same inputs → deterministic
	sig3 := d.sign(payload, secret, "2026-05-06T12:00:00Z")
	if sig1 != sig3 {
		t.Error("sign() should be deterministic for same inputs")
	}
}

func TestWebhookSign_MerchantVerification(t *testing.T) {
	// Simulate what a merchant would do to verify our webhook
	d := NewWebhookDispatcher()
	payload := []byte(`{"event":"invoice.confirmed","data":{"invoiceId":"abc-123"}}`)
	secret := "merchant-secret"
	timestamp := "2026-05-06T15:30:00Z"

	// Metapus generates signature
	signature := d.sign(payload, secret, timestamp)

	// Merchant verifies: HMAC-SHA256(timestamp + "." + body, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	if signature != expected {
		t.Errorf("merchant verification failed: got %s, want %s", signature, expected)
	}
}
