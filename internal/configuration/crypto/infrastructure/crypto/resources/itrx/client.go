package itrx

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/catalogs/crypto_provider_account"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/json"
)

const defaultBaseURL = "https://itrx.io"

type httpError struct {
	Status int
	Op     string
	Body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("itrx %s HTTP %d", e.Op, e.Status) }

func (e *httpError) Is(target error) bool {
	if target == crypto.ErrResourceProviderUnauthorized {
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	}
	if target == crypto.ErrResourceProviderTransient {
		return e.Status == http.StatusTooManyRequests || e.Status >= 500
	}
	return false
}

type Config struct {
	BaseURL     string
	APIKey      string
	APISecret   string
	CallbackURL string
	AppEnv      string
	HTTPClient  *http.Client
	Now         func() time.Time
}

func (c Config) validate() error {
	hasKey := strings.TrimSpace(c.APIKey) != ""
	hasSecret := strings.TrimSpace(c.APISecret) != ""
	if hasKey != hasSecret {
		return fmt.Errorf("API key and API secret must be configured together")
	}
	if !hasKey {
		return fmt.Errorf("iTRX API credentials are required")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("iTRX base URL is required when provider credentials are configured")
	}
	if err := crypto_provider_account.ValidateBaseURL(c.BaseURL); err != nil {
		return fmt.Errorf("iTRX base URL is invalid: %w", err)
	}
	parsed, _ := url.Parse(strings.TrimSpace(c.BaseURL))
	if strings.EqualFold(strings.TrimSpace(c.AppEnv), "production") && parsed.Scheme != "https" {
		return fmt.Errorf("iTRX base URL must use HTTPS in production")
	}
	return nil
}

type energyOrderRequest struct {
	EnergyAmount   int64  `json:"energy_amount"`
	Period         string `json:"period"`
	ReceiveAddress string `json:"receive_address"`
	Active         int    `json:"active"`
	Exclusive      int    `json:"exclusive"`
	CallbackURL    string `json:"callback_url,omitempty"`
	OutTradeNo     string `json:"out_trade_no,omitempty"`
}

type energyOrderResponse struct {
	Errno   int             `json:"errno"`
	Message string          `json:"message"`
	Serial  string          `json:"serial"`
	Amount  int64           `json:"amount"`
	Balance *int64          `json:"balance,omitempty"`
	RawJSON json.RawMessage `json:"-"`
}

type reclaimOrderRequest struct {
	Serial string `json:"serial"`
}

type reclaimOrderResponse struct {
	Errno   int             `json:"errno"`
	Message string          `json:"message"`
	RawJSON json.RawMessage `json:"-"`
}

type orderQueryResponse struct {
	Errno          int             `json:"errno"`
	Message        string          `json:"message"`
	ReceiveAddress string          `json:"receive_address"`
	OrderNo        string          `json:"order_no"`
	EnergyAmount   int64           `json:"energy_amount"`
	Amount         int64           `json:"amount"`
	Status         int             `json:"status"`
	RefundAmount   int64           `json:"refund_amount"`
	Period         int             `json:"period"`
	Balance        *int64          `json:"balance,omitempty"`
	RawJSON        json.RawMessage `json:"-"`
}

type client struct {
	baseURL    string
	apiKey     string
	apiSecret  string
	httpClient *http.Client
	now        func() time.Time
}

func newClient(cfg Config) *client {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &client{
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		apiSecret:  cfg.APISecret,
		httpClient: httpClient,
		now:        now,
	}
}

func (c *client) PlaceEnergyOrder(ctx context.Context, req energyOrderRequest) (energyOrderResponse, error) {
	var resp energyOrderResponse
	if req.EnergyAmount <= 0 || req.ReceiveAddress == "" || req.Period == "" {
		return resp, fmt.Errorf("itrx energy order requires energy_amount, receive_address, and period")
	}
	raw, err := c.doSignedPost(ctx, "/api/v1/frontend/order", req)
	if err != nil {
		return resp, err
	}
	resp.RawJSON = raw
	if err := json.Unmarshal(raw, &resp); err != nil {
		return resp, fmt.Errorf("%w: decode itrx energy order response: %v", crypto.ErrResourceProviderInvalidReply, err)
	}
	if resp.Errno != 0 {
		if resp.Message == "" {
			resp.Message = fmt.Sprintf("itrx errno %d", resp.Errno)
		}
		return resp, fmt.Errorf("itrx energy order rejected: %s", resp.Message)
	}
	if resp.Serial == "" {
		return resp, fmt.Errorf("itrx energy order returned empty serial")
	}
	return resp, nil
}

func (c *client) ReclaimOrder(ctx context.Context, serial string) (reclaimOrderResponse, error) {
	var resp reclaimOrderResponse
	if serial == "" {
		return resp, fmt.Errorf("itrx reclaim serial is required")
	}
	raw, err := c.doSignedPost(ctx, "/api/v1/frontend/order/reclaim", reclaimOrderRequest{Serial: serial})
	if err != nil {
		return resp, err
	}
	resp.RawJSON = raw
	if err := json.Unmarshal(raw, &resp); err != nil {
		return resp, fmt.Errorf("%w: decode itrx reclaim response: %v", crypto.ErrResourceProviderInvalidReply, err)
	}
	if resp.Errno != 0 {
		if resp.Message == "" {
			resp.Message = fmt.Sprintf("itrx errno %d", resp.Errno)
		}
		return resp, fmt.Errorf("itrx reclaim rejected: %s", resp.Message)
	}
	return resp, nil
}

func (c *client) QueryOrder(ctx context.Context, serial, outTradeNo string) (orderQueryResponse, error) {
	var resp orderQueryResponse
	u, err := url.Parse(c.baseURL + "/api/v1/frontend/order/query")
	if err != nil {
		return resp, fmt.Errorf("build itrx query url: %w", err)
	}
	q := u.Query()
	if serial != "" {
		q.Set("serial", serial)
	} else {
		q.Set("out_trade_no", outTradeNo)
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return resp, fmt.Errorf("build itrx query request: %w", err)
	}
	httpReq.Header.Set("API-KEY", c.apiKey)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return resp, fmt.Errorf("%w: itrx query order request: %v", crypto.ErrResourceProviderTransient, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return resp, fmt.Errorf("read itrx query order response: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return resp, &httpError{Status: httpResp.StatusCode, Op: "query order", Body: string(raw)}
	}

	resp.RawJSON = raw
	if err := json.Unmarshal(raw, &resp); err != nil {
		return resp, fmt.Errorf("%w: decode itrx query order response: %v", crypto.ErrResourceProviderInvalidReply, err)
	}
	if resp.Errno != 0 {
		if resp.Message == "" {
			resp.Message = fmt.Sprintf("itrx errno %d", resp.Errno)
		}
		if orderNotFoundMessage(resp.Message) {
			return resp, fmt.Errorf("%w: itrx query order rejected: %s", crypto.ErrResourceOrderNotFound, resp.Message)
		}
		return resp, fmt.Errorf("itrx query order rejected: %s", resp.Message)
	}
	return resp, nil
}

// orderNotFoundMessage recognizes iTRX's explicit "no such order" reply
// ("Can’t find the order.", typographic or plain apostrophe). Other rejections
// stay ambiguous.
func orderNotFoundMessage(message string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(message, "’", "'"))
	return strings.Contains(normalized, "can't find the order") || strings.Contains(normalized, "order not found") ||
		strings.Contains(normalized, "order does not exist")
}

func (c *client) doSignedPost(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	body, err := canonicalJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal itrx request: %w", err)
	}
	timestamp := strconv.FormatInt(c.now().Unix(), 10)
	signature := hmacSHA256Hex(timestamp+"&"+string(body), c.apiSecret)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build itrx request: %w", err)
	}
	httpReq.Header.Set("API-KEY", c.apiKey)
	httpReq.Header.Set("TIMESTAMP", timestamp)
	httpReq.Header.Set("SIGNATURE", signature)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: itrx signed POST %s: %v", crypto.ErrResourceProviderTransient, path, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read itrx response: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, &httpError{Status: httpResp.StatusCode, Op: path, Body: string(raw)}
	}
	return raw, nil
}

func hmacSHA256Hex(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func canonicalJSON(payload any) ([]byte, error) {
	return json.MarshalLegacyV1NoHTMLEscape(payload)
}
