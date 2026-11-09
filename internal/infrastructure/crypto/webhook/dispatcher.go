// Package crypto provides webhook notification dispatch for merchant events.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/urlsafe"
	"metapus/pkg/logger"
)

// _webhookDialTimeout limits the TCP connect phase.
const _webhookDialTimeout = 5 * time.Second

// _webhookRequestTimeout limits the entire request (connect + TLS + body).
const _webhookRequestTimeout = 10 * time.Second

const _webhookResponseLimit = int64(8 * 1024)

// HTTPDispatcher sends webhook notifications to merchants.
// Uses HMAC-SHA256 signing for payload verification.
//
// SSRF prevention (defence-in-depth):
//  1. ValidateWebhookURL at merchant create/update — rejects private IPs & dangerous hosts
//  2. ResolvePublicURL at dispatch time — resolves DNS once, validates IP, returns pinned IP
//  3. Custom DialContext with pinned IP — Go HTTP client never does its own DNS lookup
//  4. CheckRedirect blocks all redirects — prevents redirect-chain SSRF bypass
//  5. TLS ServerName set to original hostname — ensures SNI matches certificate
type HTTPDispatcher struct {
	now        func() time.Time
	resolveURL func(string, string) (*urlsafe.ResolvedURL, error)
	clientFor  func(*urlsafe.ResolvedURL) *http.Client
}

// NewDispatcher creates a new webhook dispatcher.
func NewDispatcher() *HTTPDispatcher {
	return &HTTPDispatcher{now: time.Now, resolveURL: urlsafe.ResolvePublicURL, clientFor: createPinnedClient}
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
// ActiveSecret is the merchant endpoint's current signing key.
// Returns the persisted crypto.WebhookDelivery record (always non-nil on non-repo error).
//
// SSRF-safe: resolves DNS once via ResolvePublicURL, then uses a pinned-IP
// HTTP client — Go's HTTP client never performs its own DNS lookup.
//
// Headers:
//   - Metapus-Event-Id: stable business event ID for idempotency
//   - Metapus-Delivery-Id: unique ID for this HTTP attempt
//   - Metapus-Signature: t=<unix>,v1=<HMAC-SHA256(timestamp + "." + raw body)>
func (d *HTTPDispatcher) Dispatch(
	ctx context.Context,
	deliveryRepo crypto.WebhookDeliveryRepository,
	dispatch crypto.WebhookDispatchRequest,
) (*crypto.WebhookDelivery, error) {
	if dispatch.EventID == "" {
		return nil, fmt.Errorf("webhook event ID is required")
	}
	if dispatch.Attempt < 1 {
		return nil, fmt.Errorf("webhook attempt must be positive")
	}
	if dispatch.OccurredAt.IsZero() {
		dispatch.OccurredAt = time.Now().UTC()
	}
	if dispatch.APIVersion == "" {
		dispatch.APIVersion = crypto.MerchantWebhookAPIVersion
	}
	payload := crypto.WebhookPayload{
		ID:         dispatch.EventID,
		Object:     "event",
		APIVersion: dispatch.APIVersion,
		Type:       dispatch.Event,
		CreatedAt:  dispatch.OccurredAt.UTC(),
		Data:       crypto.WebhookEventData{Object: dispatch.Data},
	}

	body := append([]byte(nil), dispatch.RawBody...)
	var err error
	if len(body) == 0 {
		body, err = json.MarshalDeterministic(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal webhook payload: %w", err)
		}
	}

	deliveredAt := d.now().UTC()
	deliveryID := "del_" + id.Compact(id.New())
	delivery := &crypto.WebhookDelivery{
		ID:          id.New(),
		InvoiceID:   dispatch.InvoiceID,
		MerchantID:  dispatch.MerchantID,
		EventType:   dispatch.Event,
		WebhookURL:  dispatch.WebhookURL,
		EventID:     dispatch.EventID,
		DeliveryID:  deliveryID,
		Attempt:     dispatch.Attempt,
		RequestBody: body,
		CreatedAt:   deliveredAt,
	}
	persist := func() error {
		if deliveryRepo == nil {
			return nil
		}
		if repoErr := deliveryRepo.Create(ctx, delivery); repoErr != nil {
			logger.Error(ctx, "failed to persist webhook delivery",
				"event_id", dispatch.EventID,
				"delivery_id", deliveryID,
				"error", repoErr,
			)
			return fmt.Errorf("persist webhook delivery: %w", repoErr)
		}
		return nil
	}

	startedAt := time.Now()
	// Resolve DNS once + validate IP — eliminates DNS rebinding TOCTOU (CWE-367).
	resolveURL := d.resolveURL
	if resolveURL == nil {
		resolveURL = urlsafe.ResolvePublicURL
	}
	resolved, err := resolveURL(dispatch.WebhookURL, "webhookUrl")
	if err != nil {
		elapsed := int(time.Since(startedAt).Milliseconds())
		errMsg := err.Error()
		delivery.ResponseTimeMs = &elapsed
		delivery.ErrorMessage = &errMsg
		delivery.ErrorClass = "url_validation_error"
		if persistErr := persist(); persistErr != nil {
			return delivery, persistErr
		}
		logger.Error(ctx, "webhook URL failed validation at dispatch time",
			"url", dispatch.WebhookURL,
			"event_id", dispatch.EventID,
			"delivery_id", deliveryID,
			"error", err,
		)
		return delivery, fmt.Errorf("webhook URL validation: %w", err)
	}

	// HMAC-SHA256 signature (Stripe-pattern: includes timestamp to prevent replay)
	signatureHeader := d.signatureHeader(body, []string{dispatch.ActiveSecret, dispatch.PreviousSecret}, deliveredAt)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dispatch.WebhookURL, bytes.NewReader(body))
	if err != nil {
		elapsed := int(time.Since(startedAt).Milliseconds())
		errMsg := err.Error()
		delivery.ResponseTimeMs = &elapsed
		delivery.ErrorMessage = &errMsg
		delivery.ErrorClass = "request_build_error"
		if persistErr := persist(); persistErr != nil {
			return delivery, persistErr
		}
		return delivery, fmt.Errorf("create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Metapus-Signature", signatureHeader)
	req.Header.Set("Metapus-Event-Id", dispatch.EventID)
	req.Header.Set("Metapus-Delivery-Id", deliveryID)
	if dispatch.Traceparent != "" {
		req.Header.Set("Traceparent", dispatch.Traceparent)
	}
	if dispatch.CorrelationID != "" {
		req.Header.Set("Metapus-Correlation-Id", dispatch.CorrelationID)
	}
	if dispatch.CausationID != "" {
		req.Header.Set("Metapus-Causation-Id", dispatch.CausationID)
	}

	// Use pinned-IP client — no second DNS lookup.
	clientFor := d.clientFor
	if clientFor == nil {
		clientFor = createPinnedClient
	}
	pinnedClient := clientFor(resolved)

	// Measure response time.
	start := time.Now()
	resp, httpErr := pinnedClient.Do(req)
	elapsed := int(time.Since(start).Milliseconds())
	delivery.ResponseTimeMs = &elapsed

	if httpErr != nil {
		errMsg := httpErr.Error()
		delivery.ErrorMessage = &errMsg
		delivery.ErrorClass = classifyWebhookError(httpErr)

		logger.Warn(ctx, "webhook delivery failed",
			"url", dispatch.WebhookURL,
			"event", dispatch.Event,
			"event_id", dispatch.EventID,
			"delivery_id", deliveryID,
			"attempt", dispatch.Attempt,
			"error", httpErr,
		)
	} else {
		defer func() { _ = resp.Body.Close() }()
		delivery.StatusCode = &resp.StatusCode
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, _webhookResponseLimit+1))
		if readErr != nil {
			delivery.ErrorClass = "response_read_error"
			delivery.ResponseBody = ""
		} else {
			if int64(len(responseBody)) > _webhookResponseLimit {
				responseBody = responseBody[:_webhookResponseLimit]
			}
			delivery.ResponseBody = sanitizeResponseExcerpt(responseBody)
		}

		if !isWebhookSuccessStatus(resp.StatusCode) {
			errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
			delivery.ErrorMessage = &errMsg
			delivery.ErrorClass = "http_error"
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
				delivery.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), deliveredAt)
			}

			logger.Warn(ctx, "webhook received non-2xx response",
				"url", dispatch.WebhookURL,
				"event", dispatch.Event,
				"event_id", dispatch.EventID,
				"delivery_id", deliveryID,
				"attempt", dispatch.Attempt,
				"status", resp.StatusCode,
			)
		} else {
			logger.Info(ctx, "webhook delivered",
				"url", dispatch.WebhookURL,
				"event", dispatch.Event,
				"event_id", dispatch.EventID,
				"delivery_id", deliveryID,
				"attempt", dispatch.Attempt,
			)
		}
	}

	if err := persist(); err != nil {
		return delivery, err
	}

	if httpErr != nil {
		return delivery, fmt.Errorf("webhook delivery: %w", httpErr)
	}
	if !isWebhookSuccessStatus(resp.StatusCode) {
		return delivery, fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}

	return delivery, nil
}

func isWebhookSuccessStatus(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func classifyWebhookError(err error) string {
	var certificateError *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var recordHeaderError tls.RecordHeaderError
	if errors.As(err, &certificateError) || errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameError) || errors.As(err, &recordHeaderError) {
		return "tls_error"
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return "dns_error"
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return "timeout"
	}
	return "network_error"
}

func sanitizeResponseExcerpt(raw []byte) string {
	value := strings.ToValidUTF8(string(raw), "�")
	var clean strings.Builder
	clean.Grow(min(len(value), int(_webhookResponseLimit)))
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' || r >= 0x20 {
			size := utf8.RuneLen(r)
			if clean.Len()+size > int(_webhookResponseLimit) {
				break
			}
			clean.WriteRune(r)
		}
	}
	return clean.String()
}

func parseRetryAfter(raw string, now time.Time) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds >= 0 {
		value := now.Add(time.Duration(seconds) * time.Second)
		return &value
	}
	if value, err := http.ParseTime(raw); err == nil {
		value = value.UTC()
		return &value
	}
	return nil
}

// sign creates an HMAC-SHA256 signature for the payload.
// Includes timestamp in the signed data to prevent replay attacks (Stripe-pattern).
// Merchant verification: HMAC-SHA256(timestamp + "." + body, secret)
func (d *HTTPDispatcher) sign(payload []byte, secret string, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func (d *HTTPDispatcher) signatureHeader(payload []byte, secrets []string, deliveredAt time.Time) string {
	timestamp := strconv.FormatInt(deliveredAt.UTC().Unix(), 10)
	header := "t=" + timestamp
	seen := make(map[string]struct{}, len(secrets))
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		if _, duplicate := seen[secret]; duplicate {
			continue
		}
		seen[secret] = struct{}{}
		header += ",v1=" + d.sign(payload, secret, timestamp)
	}
	return header
}
