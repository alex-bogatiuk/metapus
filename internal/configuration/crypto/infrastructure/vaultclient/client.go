// Package vaultclient provides the bounded HTTP transport shared by Metapus
// Vault integrations. In production it is pointed at a dedicated Vault Proxy,
// so no Vault token is required in the application process.
package vaultclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
)

// HTTPError preserves the Vault response status so callers can make narrow,
// idempotent decisions (for example, treating delete-after-delete as success).
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("vault HTTP %d: %s", e.StatusCode, e.Body)
}

func IsHTTPStatus(err error, status int) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == status
}

const (
	defaultTimeout          = 10 * time.Second
	defaultMaxRetries       = 3
	defaultRetryBaseDelay   = 500 * time.Millisecond
	defaultMaxResponseBytes = int64(1 << 20)
)

// Config configures a Vault HTTP client. Token must be empty when Addr points
// at a production Vault Proxy configured with use_auto_auth_token="force".
type Config struct {
	Addr             string
	Token            string
	HTTPClient       *http.Client
	MaxRetries       int
	RetryBaseDelay   time.Duration
	MaxResponseBytes int64
}

// Client performs JSON requests against a fixed Vault or Vault Proxy origin.
type Client struct {
	base             *url.URL
	token            string
	httpClient       *http.Client
	maxRetries       int
	retryBaseDelay   time.Duration
	maxResponseBytes int64
}

// New validates cfg and constructs a reusable client.
func New(cfg Config) (*Client, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.Addr))
	if err != nil {
		return nil, fmt.Errorf("parse Vault address: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("vault address must use http or https")
	}
	if base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("vault address must be an origin without credentials, query, or fragment")
	}
	base.Path = strings.TrimRight(base.Path, "/")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = defaultMaxRetries
	}
	if maxRetries < 1 {
		return nil, fmt.Errorf("vault max retries must be positive")
	}
	retryBaseDelay := cfg.RetryBaseDelay
	if retryBaseDelay == 0 {
		retryBaseDelay = defaultRetryBaseDelay
	}
	if retryBaseDelay < 0 {
		return nil, fmt.Errorf("vault retry delay cannot be negative")
	}
	maxResponseBytes := cfg.MaxResponseBytes
	if maxResponseBytes == 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}
	if maxResponseBytes < 1 {
		return nil, fmt.Errorf("vault response limit must be positive")
	}

	return &Client{
		base:             base,
		token:            strings.TrimSpace(cfg.Token),
		httpClient:       httpClient,
		maxRetries:       maxRetries,
		retryBaseDelay:   retryBaseDelay,
		maxResponseBytes: maxResponseBytes,
	}, nil
}

// DoJSON sends a request to an absolute Vault API path such as
// /v1/transit/sign/key. It retries transport failures and 5xx responses only.
func (c *Client) DoJSON(ctx context.Context, method, apiPath string, requestBody, responseTarget any) error {
	endpoint, err := c.endpoint(apiPath)
	if err != nil {
		return err
	}

	var body []byte
	if requestBody != nil {
		body, err = json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal Vault request: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt < c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryBaseDelay * time.Duration(1<<uint(attempt-1))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return fmt.Errorf("build Vault request: %w", err)
		}
		if c.token != "" {
			req.Header.Set("X-Vault-Token", c.token)
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBody, readErr := readBounded(resp.Body, c.maxResponseBytes)
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Vault response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: responseExcerpt(respBody)}
			if resp.StatusCode < 500 {
				return httpErr
			}
			lastErr = httpErr
			continue
		}
		if responseTarget == nil || len(bytes.TrimSpace(respBody)) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, responseTarget); err != nil {
			return fmt.Errorf("decode Vault response: %w", err)
		}
		return nil
	}

	return fmt.Errorf("vault request failed after %d attempts: %w", c.maxRetries, lastErr)
}

func (c *Client) endpoint(apiPath string) (string, error) {
	if !strings.HasPrefix(apiPath, "/v1/") || strings.ContainsAny(apiPath, "?#") {
		return "", fmt.Errorf("invalid Vault API path")
	}
	u := *c.base
	u.Path = c.base.Path + apiPath
	return u.String(), nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(r, limit+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return payload, nil
}

func responseExcerpt(payload []byte) string {
	payload = bytes.TrimSpace(payload)
	if len(payload) > 200 {
		payload = payload[:200]
	}
	return string(payload)
}
