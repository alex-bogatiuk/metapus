// Package crypto provides webhook notification dispatch for merchant events.
package crypto

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/urlsafe"
	"metapus/pkg/logger"
)

// WebhookEventType defines the types of events that trigger merchant webhooks.
type WebhookEventType string

const (
	WebhookInvoicePaid           WebhookEventType = "invoice.paid"
	WebhookInvoiceConfirmed      WebhookEventType = "invoice.confirmed"
	WebhookInvoiceExpired        WebhookEventType = "invoice.expired"
	WebhookWithdrawalConfirmed   WebhookEventType = "withdrawal.confirmed"
)

// WebhookPayload is the payload sent to merchant's webhook URL.
type WebhookPayload struct {
	Event     WebhookEventType       `json:"event"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// WebhookDispatcher sends webhook notifications to merchants.
// Uses HMAC-SHA256 signing for payload verification.
type WebhookDispatcher struct {
	httpClient *http.Client
}

// NewWebhookDispatcher creates a new webhook dispatcher.
// CheckRedirect blocks all redirects to prevent SSRF bypass via redirect chains.
// Scenario: attacker sets callback to https://legit.com/redirect?to=http://169.254.169.254
// Without this, the initial URL passes validation but the redirect reaches cloud metadata.
func NewWebhookDispatcher() *WebhookDispatcher {
	return &WebhookDispatcher{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse // block all redirects
			},
		},
	}
}

// ValidateWebhookURL validates that a webhook URL is safe to call.
// Delegates to core/urlsafe for full SSRF prevention including DNS resolution.
//
// Call this during merchant Create/Update validation.
func ValidateWebhookURL(rawURL string) error {
	return urlsafe.ValidatePublicURL(rawURL, "webhookUrl")
}

// Dispatch sends a webhook event to the given URL with HMAC signature.
// webhookSecret is the merchant's webhook signing key.
//
// Defence-in-depth: validates URL before making the request, even though
// ValidateWebhookURL should have been called during merchant creation.
//
// Headers:
//   - X-Metapus-Event: event type
//   - X-Metapus-Signature: HMAC-SHA256(timestamp + "." + payload, secret) (Stripe-pattern)
//   - X-Metapus-Timestamp: RFC3339 timestamp (include in HMAC to prevent replay)
//   - X-Metapus-Delivery-ID: unique delivery ID for idempotency
func (d *WebhookDispatcher) Dispatch(
	ctx context.Context,
	webhookURL string,
	webhookSecret string,
	event WebhookEventType,
	data map[string]interface{},
) error {
	// Defence-in-depth: re-validate URL at dispatch time
	if err := ValidateWebhookURL(webhookURL); err != nil {
		logger.Error(ctx, "webhook URL failed validation at dispatch time",
			"url", webhookURL,
			"error", err,
		)
		return fmt.Errorf("webhook URL validation: %w", err)
	}

	payload := WebhookPayload{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	// HMAC-SHA256 signature (Stripe-pattern: includes timestamp to prevent replay)
	timestampStr := payload.Timestamp.Format(time.RFC3339)
	signature := d.sign(body, webhookSecret, timestampStr)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}

	deliveryID := id.New().String()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Metapus-Event", string(event))
	req.Header.Set("X-Metapus-Signature", signature)
	req.Header.Set("X-Metapus-Timestamp", payload.Timestamp.Format(time.RFC3339))
	req.Header.Set("X-Metapus-Delivery-ID", deliveryID)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		logger.Warn(ctx, "webhook delivery failed",
			"url", webhookURL,
			"event", event,
			"delivery_id", deliveryID,
			"error", err,
		)
		return fmt.Errorf("webhook delivery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		logger.Warn(ctx, "webhook received non-2xx response",
			"url", webhookURL,
			"event", event,
			"delivery_id", deliveryID,
			"status", resp.StatusCode,
		)
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}

	logger.Info(ctx, "webhook delivered",
		"url", webhookURL,
		"event", event,
		"delivery_id", deliveryID,
	)

	return nil
}

// sign creates an HMAC-SHA256 signature for the payload.
// Includes timestamp in the signed data to prevent replay attacks (Stripe-pattern).
// Merchant verification: HMAC-SHA256(timestamp + "." + body, secret)
func (d *WebhookDispatcher) sign(payload []byte, secret string, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
