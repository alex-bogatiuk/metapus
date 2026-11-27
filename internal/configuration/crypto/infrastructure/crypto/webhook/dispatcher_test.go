// internal/configuration/crypto/domain/crypto/webhook_test.go
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/urlsafe"
)

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		give    string
		url     string
		wantErr bool
	}{
		// Valid
		{"valid HTTPS URL", "https://1.1.1.1/webhook", false},
		{"valid HTTPS with path and port", "https://8.8.8.8:8443/v1/webhooks/crypto", false},
		{"empty URL (no webhook configured)", "", false},

		// Protocol violations
		{"HTTP not allowed", "http://merchant.example.com/webhook", true},
		{"FTP not allowed", "ftp://merchant.example.com/webhook", true},
		{"non-webhook port", "https://1.1.1.1:8080/webhook", true},
		{"userinfo credentials", "https://user:password@1.1.1.1/webhook", true},

		// Loopback / private IPs
		{"loopback IPv4", "https://127.0.0.1/webhook", true},
		{"loopback IPv6", "https://[::1]/webhook", true},
		{"private 10.x", "https://10.0.0.1/webhook", true},
		{"private 172.16.x", "https://172.16.0.1/webhook", true},
		{"private 192.168.x", "https://192.168.1.1/webhook", true},

		// Cloud metadata SSRF
		{"AWS metadata IPv4", "https://169.254.169.254/latest/meta-data/", true},
		{"carrier grade NAT", "https://100.64.0.1/webhook", true},
		{"documentation IPv4", "https://203.0.113.10/webhook", true},
		{"documentation IPv6", "https://[2001:db8::1]/webhook", true},

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

type webhookTimeoutError struct{}

func (webhookTimeoutError) Error() string   { return "timeout" }
func (webhookTimeoutError) Timeout() bool   { return true }
func (webhookTimeoutError) Temporary() bool { return true }

type webhookRoundTripFunc func(*http.Request) (*http.Response, error)

func (f webhookRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type captureWebhookDeliveryRepo struct {
	deliveries []crypto.WebhookDelivery
}

func (r *captureWebhookDeliveryRepo) Create(_ context.Context, delivery *crypto.WebhookDelivery) error {
	copy := *delivery
	copy.RequestBody = append([]byte(nil), delivery.RequestBody...)
	r.deliveries = append(r.deliveries, copy)
	return nil
}

func (*captureWebhookDeliveryRepo) SetNextRetry(context.Context, id.ID, *time.Time) error {
	return nil
}

func (*captureWebhookDeliveryRepo) ListByInvoice(context.Context, id.ID, int, int) ([]crypto.WebhookDelivery, int, error) {
	return nil, 0, nil
}

func (*captureWebhookDeliveryRepo) ListByMerchant(context.Context, id.ID, int, int) ([]crypto.WebhookDelivery, int, error) {
	return nil, 0, nil
}

func TestWebhookDeliveryResponseAndErrorClassification(t *testing.T) {
	for status, want := range map[int]bool{199: false, 200: true, 204: true, 299: true, 300: false, 500: false} {
		if got := isWebhookSuccessStatus(status); got != want {
			t.Fatalf("isWebhookSuccessStatus(%d) = %t, want %t", status, got, want)
		}
	}
	if got := classifyWebhookError(webhookTimeoutError{}); got != "timeout" {
		t.Fatalf("timeout class = %q", got)
	}
	if got := classifyWebhookError(&net.DNSError{Err: "not found", Name: "merchant.example"}); got != "dns_error" {
		t.Fatalf("DNS class = %q", got)
	}
	if got := classifyWebhookError(x509.UnknownAuthorityError{}); got != "tls_error" {
		t.Fatalf("TLS class = %q", got)
	}
}

func TestWebhookResponseExcerptIsSanitizedAndBounded(t *testing.T) {
	raw := append([]byte("ok\x00secret\x01\n"), []byte(strings.Repeat("x", 10*1024))...)
	got := sanitizeResponseExcerpt(raw)
	if strings.ContainsRune(got, '\x00') || strings.ContainsRune(got, '\x01') {
		t.Fatalf("response excerpt retained control characters: %q", got[:20])
	}
	if len([]byte(got)) > int(_webhookResponseLimit) {
		t.Fatalf("response excerpt is %d bytes, limit is %d", len([]byte(got)), _webhookResponseLimit)
	}
}

func TestParseRetryAfterSupportsDeltaAndHTTPDate(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("120", now); got == nil || !got.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("delta Retry-After = %v", got)
	}
	want := now.Add(10 * time.Minute)
	if got := parseRetryAfter(want.Format(http.TimeFormat), now); got == nil || !got.Equal(want) {
		t.Fatalf("date Retry-After = %v, want %v", got, want)
	}
	if got := parseRetryAfter("invalid", now); got != nil {
		t.Fatalf("invalid Retry-After = %v, want nil", got)
	}
}

func TestWebhookSignatureHeaderUsesDeliveryTimestampAndRotationSecrets(t *testing.T) {
	d := NewDispatcher()
	body := []byte(`{"id":"evt_test"}`)
	first := d.signatureHeader(body, []string{"active", "previous"}, time.Unix(100, 0))
	second := d.signatureHeader(body, []string{"active", "previous"}, time.Unix(200, 0))
	if first == second {
		t.Fatal("delivery retries must receive a fresh timestamp and signature")
	}
	if want := "t=100,"; len(first) < len(want) || first[:len(want)] != want {
		t.Fatalf("signature header = %q, want prefix %q", first, want)
	}
	if count := strings.Count(first, "v1="); count != 2 {
		t.Fatalf("rotation signature count = %d, want 2", count)
	}
}

func TestWebhookDispatchRetriesKeepBodyAndEventButRefreshAttemptHeaders(t *testing.T) {
	type capturedRequest struct {
		body, eventID, deliveryID, signature    string
		traceparent, correlationID, causationID string
	}
	var requests []capturedRequest
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, capturedRequest{
			body: string(body), eventID: request.Header.Get("Metapus-Event-Id"),
			deliveryID: request.Header.Get("Metapus-Delivery-Id"), signature: request.Header.Get("Metapus-Signature"),
			traceparent: request.Header.Get("Traceparent"), correlationID: request.Header.Get("Metapus-Correlation-Id"),
			causationID: request.Header.Get("Metapus-Causation-Id"),
		})
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_787_724_723, 0)
	dispatcher := NewDispatcher()
	dispatcher.now = func() time.Time { return now }
	dispatcher.resolveURL = func(string, string) (*urlsafe.ResolvedURL, error) {
		return &urlsafe.ResolvedURL{Parsed: parsed, ResolvedIP: "127.0.0.1", Host: parsed.Hostname()}, nil
	}
	dispatcher.clientFor = func(*urlsafe.ResolvedURL) *http.Client { return server.Client() }
	repo := &captureWebhookDeliveryRepo{}
	request := crypto.WebhookDispatchRequest{
		MerchantID: id.New(), WebhookURL: server.URL, ActiveSecret: "active", PreviousSecret: "previous",
		EventID: "evt_retry", Event: crypto.WebhookInvoiceCompleted, OccurredAt: now.Add(-time.Minute),
		Data: map[string]any{"id": "inv_test", "status": "completed"}, Attempt: 1,
		Traceparent:   "00-abababababababababababababababab-cdcdcdcdcdcdcdcd-01",
		CorrelationID: "request-123", CausationID: "payment-456",
	}
	if _, err := dispatcher.Dispatch(context.Background(), repo, request); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	request.Attempt = 2
	if _, err := dispatcher.Dispatch(context.Background(), repo, request); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || len(repo.deliveries) != 2 {
		t.Fatalf("requests/deliveries = %d/%d, want 2/2", len(requests), len(repo.deliveries))
	}
	if requests[0].body != requests[1].body || requests[0].eventID != requests[1].eventID {
		t.Fatal("retry changed immutable body or event ID")
	}
	if requests[0].deliveryID == requests[1].deliveryID || requests[0].signature == requests[1].signature {
		t.Fatal("retry did not refresh delivery ID and timestamped signature")
	}
	for _, captured := range requests {
		if captured.traceparent != request.Traceparent || captured.correlationID != request.CorrelationID ||
			captured.causationID != request.CausationID {
			t.Fatalf("webhook lost durable trace context: %+v", captured)
		}
	}
	if strings.Count(requests[0].signature, "v1=") != 2 {
		t.Fatalf("dual signature header = %q", requests[0].signature)
	}
}

func TestWebhookDispatchClassifiesRetryableHTTPAndTimeoutFailures(t *testing.T) {
	now := time.Unix(1_787_724_723, 0)
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		wantRetry  bool
	}{
		{name: "429", status: http.StatusTooManyRequests, retryAfter: "120", wantRetry: true},
		{name: "503", status: http.StatusServiceUnavailable, retryAfter: "120", wantRetry: true},
		{name: "500", status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("temporary failure"))
			}))
			defer server.Close()
			parsed, _ := url.Parse(server.URL)
			dispatcher := NewDispatcher()
			dispatcher.now = func() time.Time { return now }
			dispatcher.resolveURL = func(string, string) (*urlsafe.ResolvedURL, error) {
				return &urlsafe.ResolvedURL{Parsed: parsed, ResolvedIP: "127.0.0.1", Host: parsed.Hostname()}, nil
			}
			dispatcher.clientFor = func(*urlsafe.ResolvedURL) *http.Client { return server.Client() }
			repo := &captureWebhookDeliveryRepo{}
			delivery, err := dispatcher.Dispatch(context.Background(), repo, crypto.WebhookDispatchRequest{
				MerchantID: id.New(), WebhookURL: server.URL, ActiveSecret: "secret", EventID: "evt_failure",
				Event: crypto.WebhookInvoicePaid, OccurredAt: now, Data: map[string]any{"id": "inv_test"}, Attempt: 1,
			})
			if err == nil || delivery == nil || delivery.ErrorClass != "http_error" {
				t.Fatalf("delivery/error = %#v/%v", delivery, err)
			}
			if (delivery.RetryAfter != nil) != test.wantRetry {
				t.Fatalf("RetryAfter = %v, want present=%t", delivery.RetryAfter, test.wantRetry)
			}
		})
	}

	parsed, _ := url.Parse("https://merchant.example/webhook")
	dispatcher := NewDispatcher()
	dispatcher.now = func() time.Time { return now }
	dispatcher.resolveURL = func(string, string) (*urlsafe.ResolvedURL, error) {
		return &urlsafe.ResolvedURL{Parsed: parsed, ResolvedIP: "1.1.1.1", Host: parsed.Hostname()}, nil
	}
	dispatcher.clientFor = func(*urlsafe.ResolvedURL) *http.Client {
		return &http.Client{Transport: webhookRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, webhookTimeoutError{}
		})}
	}
	repo := &captureWebhookDeliveryRepo{}
	delivery, err := dispatcher.Dispatch(context.Background(), repo, crypto.WebhookDispatchRequest{
		MerchantID: id.New(), WebhookURL: parsed.String(), ActiveSecret: "secret", EventID: "evt_timeout",
		Event: crypto.WebhookInvoicePaid, OccurredAt: now, Data: map[string]any{"id": "inv_test"}, Attempt: 1,
	})
	if err == nil || delivery == nil || delivery.ErrorClass != "timeout" {
		t.Fatalf("timeout delivery/error = %#v/%v", delivery, err)
	}
}

func TestPinnedWebhookClientIgnoresAReboundDialAddressAndBlocksRedirects(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
	}()

	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	parsed, err := url.Parse("https://merchant.example:" + port + "/webhook")
	if err != nil {
		t.Fatal(err)
	}
	client := createPinnedClient(&urlsafe.ResolvedURL{
		Parsed: parsed, ResolvedIP: "127.0.0.1", Host: parsed.Hostname(),
	})
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS minimum = %#x, want TLS 1.2", transport.TLSClientConfig.MinVersion)
	}
	if !transport.DisableKeepAlives {
		t.Fatal("pinned transport keeps idle connections; want one connection per delivery")
	}

	accepted := make(chan struct{}, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = connection.Close()
			accepted <- struct{}{}
		}
	}()
	connection, err := transport.DialContext(context.Background(), "tcp", "169.254.169.254:443")
	if err != nil {
		t.Fatalf("pinned dial: %v", err)
	}
	_ = connection.Close()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("dial did not reach the pre-validated pinned address")
	}

	redirectErr := client.CheckRedirect(&http.Request{}, nil)
	if !errors.Is(redirectErr, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v, want http.ErrUseLastResponse", redirectErr)
	}
}

func TestWebhookDispatchBlocksRedirectAndBoundsActualResponse(t *testing.T) {
	var server *httptest.Server
	targetHits := 0
	server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/redirect":
			response.Header().Set("Location", server.URL+"/target")
			response.WriteHeader(http.StatusFound)
		case "/target":
			targetHits++
			response.WriteHeader(http.StatusNoContent)
		default:
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(strings.Repeat("x", 10*1024)))
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	newDispatcher := func(path string) *HTTPDispatcher {
		dispatcher := NewDispatcher()
		dispatcher.resolveURL = func(string, string) (*urlsafe.ResolvedURL, error) {
			copy := *parsed
			copy.Path = path
			return &urlsafe.ResolvedURL{Parsed: &copy, ResolvedIP: "127.0.0.1", Host: parsed.Hostname()}, nil
		}
		dispatcher.clientFor = func(resolved *urlsafe.ResolvedURL) *http.Client {
			client := server.Client()
			client.CheckRedirect = createPinnedClient(resolved).CheckRedirect
			return client
		}
		return dispatcher
	}

	repo := &captureWebhookDeliveryRepo{}
	redirectDelivery, err := newDispatcher("/redirect").Dispatch(context.Background(), repo, crypto.WebhookDispatchRequest{
		MerchantID: id.New(), WebhookURL: server.URL + "/redirect", ActiveSecret: "secret",
		EventID: "evt_redirect", Event: crypto.WebhookInvoiceCompleted, OccurredAt: time.Now(), Attempt: 1,
	})
	if err == nil || redirectDelivery == nil || redirectDelivery.StatusCode == nil || *redirectDelivery.StatusCode != http.StatusFound {
		t.Fatalf("redirect delivery/error = %#v/%v", redirectDelivery, err)
	}
	if targetHits != 0 {
		t.Fatalf("redirect target received %d requests, want 0", targetHits)
	}

	oversizedDelivery, err := newDispatcher("/oversized").Dispatch(context.Background(), repo, crypto.WebhookDispatchRequest{
		MerchantID: id.New(), WebhookURL: server.URL + "/oversized", ActiveSecret: "secret",
		EventID: "evt_oversized", Event: crypto.WebhookInvoiceCompleted, OccurredAt: time.Now(), Attempt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len([]byte(oversizedDelivery.ResponseBody)) != int(_webhookResponseLimit) {
		t.Fatalf("response excerpt = %d bytes, want %d", len([]byte(oversizedDelivery.ResponseBody)), _webhookResponseLimit)
	}
}

func TestWebhookDispatchRejectsUntrustedTLSCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	dispatcher := NewDispatcher()
	dispatcher.resolveURL = func(string, string) (*urlsafe.ResolvedURL, error) {
		return &urlsafe.ResolvedURL{Parsed: parsed, ResolvedIP: "127.0.0.1", Host: parsed.Hostname()}, nil
	}
	repo := &captureWebhookDeliveryRepo{}
	delivery, err := dispatcher.Dispatch(context.Background(), repo, crypto.WebhookDispatchRequest{
		MerchantID: id.New(), WebhookURL: server.URL, ActiveSecret: "secret",
		EventID: "evt_tls", Event: crypto.WebhookInvoiceCompleted, OccurredAt: time.Now(), Attempt: 1,
	})
	if err == nil || delivery == nil || delivery.ErrorClass != "tls_error" {
		t.Fatalf("TLS delivery/error = %#v/%v", delivery, err)
	}
}

func TestWebhookSign_IncludesTimestamp(t *testing.T) {
	d := NewDispatcher()
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
	d := NewDispatcher()
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
