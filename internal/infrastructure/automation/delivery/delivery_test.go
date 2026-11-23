package delivery

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/infrastructure/urlsafe"
)

func TestWebhookAdapterRejectsUnsafeURL(t *testing.T) {
	adapter := NewWebhookAdapter()

	cases := []struct {
		name string
		url  string
	}{
		{name: "plain HTTP", url: "http://example.com/hook"},
		{name: "loopback IP", url: "https://127.0.0.1/hook"},
		{name: "localhost", url: "https://localhost/hook"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := adapter.Deliver(
				context.Background(),
				map[string]any{"url": tc.url},
				nil,
				nil,
				`{"ok":true}`,
				nil,
			)
			if err == nil {
				t.Fatalf("expected validation error for %q", tc.url)
			}
		})
	}
}

func TestPinnedAutomationWebhookClientUsesResolvedIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("split test server host: %v", err)
	}

	parsed := mustParseURL(t, fmt.Sprintf("http://unresolvable.invalid:%s/hook", port))
	client := newPinnedAutomationWebhookClient(&urlsafe.ResolvedURL{
		Parsed:     parsed,
		ResolvedIP: "127.0.0.1",
		Host:       "unresolvable.invalid",
	})

	req, err := http.NewRequest(http.MethodPost, parsed.String(), strings.NewReader(`{"ok":true}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected pinned client to bypass DNS and reach test server: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestWebhookAdapterDoesNotFollowRedirects(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "http://127.0.0.1/internal", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("split test server host: %v", err)
	}

	parsed := mustParseURL(t, fmt.Sprintf("http://example.com:%s/hook", port))
	adapter := &WebhookAdapter{
		resolveURL: func(_ string, _ string) (*urlsafe.ResolvedURL, error) {
			return &urlsafe.ResolvedURL{
				Parsed:     parsed,
				ResolvedIP: "127.0.0.1",
				Host:       "example.com",
			}, nil
		},
		clientFactory: newPinnedAutomationWebhookClient,
	}

	err = adapter.Deliver(
		context.Background(),
		map[string]any{"url": parsed.String()},
		nil,
		nil,
		`{"ok":true}`,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "unexpected status 307") {
		t.Fatalf("Deliver error = %v, want unexpected status 307", err)
	}

	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 redirect response without follow-up request", got)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}
