// Package crypto provides webhook notification dispatch for merchant events.
package crypto

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/urlsafe"
	"metapus/pkg/logger"
)

// WebhookEventType defines the types of events that trigger merchant webhooks.
type WebhookEventType string

const (
	WebhookInvoicePaid         WebhookEventType = "invoice.paid"
	WebhookInvoiceConfirmed    WebhookEventType = "invoice.confirmed"
	WebhookInvoiceExpired      WebhookEventType = "invoice.expired"
	WebhookWithdrawalConfirmed WebhookEventType = "withdrawal.confirmed"
)

// _webhookDialTimeout limits the TCP connect phase.
const _webhookDialTimeout = 5 * time.Second

// _webhookRequestTimeout limits the entire request (connect + TLS + body).
const _webhookRequestTimeout = 10 * time.Second

// WebhookPayload is the payload sent to merchant's webhook URL.
type WebhookPayload struct {
	Event     WebhookEventType `json:"event"`
	Timestamp time.Time        `json:"timestamp"`
	Data      map[string]any   `json:"data"`
}

// WebhookDispatcher sends webhook notifications to merchants.
// Uses HMAC-SHA256 signing for payload verification.
//
// SSRF prevention (defence-in-depth):
//  1. ValidateWebhookURL at merchant create/update — rejects private IPs & dangerous hosts
//  2. ResolvePublicURL at dispatch time — resolves DNS once, validates IP, returns pinned IP
//  3. Custom DialContext with pinned IP — Go HTTP client never does its own DNS lookup
//  4. CheckRedirect blocks all redirects — prevents redirect-chain SSRF bypass
//  5. TLS ServerName set to original hostname — ensures SNI matches certificate
type WebhookDispatcher struct{}

// NewWebhookDispatcher creates a new webhook dispatcher.
func NewWebhookDispatcher() *WebhookDispatcher {
	return &WebhookDispatcher{}
}

// ValidateWebhookURL validates that a webhook URL is safe to call.
// Delegates to core/urlsafe for full SSRF prevention including DNS resolution.
//
// Call this during merchant Create/Update validation.
func ValidateWebhookURL(rawURL string) error {
	return urlsafe.ValidatePublicURL(rawURL, "webhookUrl")
}

// createPinnedClient builds an http.Client that connects only to the given
// pre-resolved IP, bypassing the Go net package's DNS resolver entirely.
// This eliminates the DNS rebinding TOCTOU vulnerability (CWE-367/CWE-918):
// without this, ValidatePublicURL resolves DNS for checking, and http.Client.Do
// resolves DNS again — an attacker with TTL=0 can return a private IP on the
// second lookup.
func createPinnedClient(resolved *urlsafe.ResolvedURL) *http.Client {
	port := resolved.Parsed.Port()
	if port == "" {
		port = "443" // HTTPS-only (enforced by ResolvePublicURL)
	}

	pinnedAddr := net.JoinHostPort(resolved.ResolvedIP, port)

	return &http.Client{
		Timeout: _webhookRequestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse // block all redirects
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				// Ignore the addr parameter (which would trigger DNS).
				// Connect directly to the pre-validated public IP.
				return (&net.Dialer{Timeout: _webhookDialTimeout}).DialContext(ctx, network, pinnedAddr)
			},
			TLSClientConfig: &tls.Config{
				ServerName: resolved.Host, // SNI must match the certificate
				MinVersion: tls.VersionTLS12,
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          1,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// Dispatch sends a webhook event to the given URL with HMAC signature and records the delivery.
// webhookSecret is the merchant's webhook signing key.
// Returns the persisted WebhookDelivery record (always non-nil on non-repo error).
//
// SSRF-safe: resolves DNS once via ResolvePublicURL, then uses a pinned-IP
// HTTP client — Go's HTTP client never performs its own DNS lookup.
//
// Headers:
//   - X-Metapus-Event: event type
//   - X-Metapus-Signature: HMAC-SHA256(timestamp + "." + payload, secret) (Stripe-pattern)
//   - X-Metapus-Timestamp: RFC3339 timestamp (include in HMAC to prevent replay)
//   - X-Metapus-Delivery-ID: unique delivery ID for idempotency
func (d *WebhookDispatcher) Dispatch(
	ctx context.Context,
	deliveryRepo WebhookDeliveryRepository,
	invoiceID *id.ID,
	merchantID id.ID,
	webhookURL string,
	webhookSecret string,
	event WebhookEventType,
	data map[string]any,
	attempt int,
) (*WebhookDelivery, error) {
	// Resolve DNS once + validate IP — eliminates DNS rebinding TOCTOU (CWE-367).
	resolved, err := urlsafe.ResolvePublicURL(webhookURL, "webhookUrl")
	if err != nil {
		logger.Error(ctx, "webhook URL failed validation at dispatch time",
			"url", webhookURL,
			"error", err,
		)
		return nil, fmt.Errorf("webhook URL validation: %w", err)
	}

	payload := WebhookPayload{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook payload: %w", err)
	}

	// HMAC-SHA256 signature (Stripe-pattern: includes timestamp to prevent replay)
	timestampStr := payload.Timestamp.Format(time.RFC3339)
	signature := d.sign(body, webhookSecret, timestampStr)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create webhook request: %w", err)
	}

	deliveryIDStr := id.New().String()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Metapus-Event", string(event))
	req.Header.Set("X-Metapus-Signature", signature)
	req.Header.Set("X-Metapus-Timestamp", payload.Timestamp.Format(time.RFC3339))
	req.Header.Set("X-Metapus-Delivery-ID", deliveryIDStr)

	// Use pinned-IP client — no second DNS lookup.
	pinnedClient := createPinnedClient(resolved)

	// Measure response time.
	start := time.Now()
	resp, httpErr := pinnedClient.Do(req)
	elapsed := int(time.Since(start).Milliseconds())

	// Build delivery record (always persisted, success or failure).
	delivery := &WebhookDelivery{
		ID:             id.New(),
		InvoiceID:      invoiceID,
		MerchantID:     merchantID,
		EventType:      event,
		WebhookURL:     webhookURL,
		DeliveryID:     deliveryIDStr,
		ResponseTimeMs: &elapsed,
		Attempt:        attempt,
		RequestBody:    body,
		CreatedAt:      time.Now().UTC(),
	}

	if httpErr != nil {
		errMsg := httpErr.Error()
		delivery.ErrorMessage = &errMsg

		logger.Warn(ctx, "webhook delivery failed",
			"url", webhookURL,
			"event", event,
			"delivery_id", deliveryIDStr,
			"attempt", attempt,
			"error", httpErr,
		)
	} else {
		defer func() { _ = resp.Body.Close() }()
		delivery.StatusCode = &resp.StatusCode

		if resp.StatusCode >= 300 {
			errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
			delivery.ErrorMessage = &errMsg

			logger.Warn(ctx, "webhook received non-2xx response",
				"url", webhookURL,
				"event", event,
				"delivery_id", deliveryIDStr,
				"attempt", attempt,
				"status", resp.StatusCode,
			)
		} else {
			logger.Info(ctx, "webhook delivered",
				"url", webhookURL,
				"event", event,
				"delivery_id", deliveryIDStr,
				"attempt", attempt,
			)
		}
	}

	// Persist the delivery record.
	if deliveryRepo != nil {
		if repoErr := deliveryRepo.Create(ctx, delivery); repoErr != nil {
			logger.Error(ctx, "failed to persist webhook delivery",
				"delivery_id", deliveryIDStr,
				"error", repoErr,
			)
			// Don't fail the caller — delivery persistence is best-effort audit.
		}
	}

	if httpErr != nil {
		return delivery, fmt.Errorf("webhook delivery: %w", httpErr)
	}
	if resp.StatusCode >= 300 {
		return delivery, fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}

	return delivery, nil
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
