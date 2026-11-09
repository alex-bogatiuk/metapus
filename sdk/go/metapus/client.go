package metapus

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL, APIKey string
	HTTPClient      *http.Client
}

func (c *Client) CreateInvoice(ctx context.Context, idempotencyKey string, request any, response any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/invoices"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	if idempotencyKey == "" {
		idempotencyKey, err = newIdempotencyKey()
		if err != nil {
			return err
		}
	}
	req.Header.Set("Idempotency-Key", idempotencyKey)
	return c.do(req, response)
}

// BindInvoiceReference reuses the supplied key across retries of this request.
func (c *Client) BindInvoiceReference(ctx context.Context, invoiceID, idempotencyKey string, request BindInvoiceReferenceRequest) (*Invoice, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, fmt.Errorf("Idempotency-Key is required")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.url("/invoices/"+url.PathEscape(invoiceID)+"/reference"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Idempotency-Key", idempotencyKey)
	var invoice Invoice
	if err := c.do(req, &invoice); err != nil {
		return nil, err
	}
	return &invoice, nil
}

func (c *Client) GetInvoice(ctx context.Context, invoiceID string, response any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/invoices/"+url.PathEscape(invoiceID)), nil)
	if err != nil {
		return err
	}
	return c.do(req, response)
}

func (c *Client) CancelInvoice(ctx context.Context, invoiceID string) error {
	return c.requestJSON(ctx, http.MethodPost, "/invoices/"+url.PathEscape(invoiceID)+"/cancel", nil, nil)
}

func (c *Client) ListWebhookEndpoints(ctx context.Context, response any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/webhook-endpoints"), nil)
	if err != nil {
		return err
	}
	return c.do(req, response)
}

func (c *Client) CreateWebhookEndpoint(ctx context.Context, request CreateWebhookEndpointRequest, response any) error {
	return c.requestJSON(ctx, http.MethodPost, "/webhook-endpoints", request, response)
}

func (c *Client) GetWebhookEndpoint(ctx context.Context, endpointID string, response any) error {
	return c.get(ctx, "/webhook-endpoints/"+url.PathEscape(endpointID), response)
}

func (c *Client) UpdateWebhookEndpoint(ctx context.Context, endpointID string, request UpdateWebhookEndpointRequest, response any) error {
	return c.requestJSON(ctx, http.MethodPatch, "/webhook-endpoints/"+url.PathEscape(endpointID), request, response)
}

func (c *Client) DeleteWebhookEndpoint(ctx context.Context, endpointID string) error {
	return c.requestJSON(ctx, http.MethodDelete, "/webhook-endpoints/"+url.PathEscape(endpointID), nil, nil)
}

func (c *Client) RotateWebhookSecret(ctx context.Context, endpointID string, response any) error {
	return c.requestJSON(ctx, http.MethodPost, "/webhook-endpoints/"+url.PathEscape(endpointID)+"/rotate-secret", nil, response)
}

func (c *Client) TestWebhookEndpoint(ctx context.Context, endpointID string, response *WebhookTestResult) error {
	return c.requestJSON(ctx, http.MethodPost, "/webhook-endpoints/"+url.PathEscape(endpointID)+"/test", nil, response)
}

func (c *Client) ListEvents(ctx context.Context, limit, offset int, response any) error {
	return c.get(ctx, fmt.Sprintf("/events?limit=%d&offset=%d", limit, offset), response)
}

func (c *Client) ListEventDeliveries(ctx context.Context, eventID string, response any) error {
	return c.get(ctx, "/events/"+url.PathEscape(eventID)+"/deliveries", response)
}

func (c *Client) RedeliverEvent(ctx context.Context, eventID, endpointID string, response any) error {
	return c.requestJSON(ctx, http.MethodPost, "/events/"+url.PathEscape(eventID)+"/redeliver", map[string]string{"endpointId": endpointID}, response)
}

func (c *Client) get(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return err
	}
	return c.do(req, target)
}

func (c *Client) requestJSON(ctx context.Context, method, path string, request, target any) error {
	var body io.Reader
	if request != nil {
		encoded, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return err
	}
	key, err := newIdempotencyKey()
	if err != nil {
		return err
	}
	req.Header.Set("Idempotency-Key", key)
	return c.do(req, target)
}

func newIdempotencyKey() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

func (c *Client) do(req *http.Request, target any) error {
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		excerpt, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return fmt.Errorf("Metapus API returned %d: %s", response.StatusCode, excerpt)
	}
	if target == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func (c *Client) url(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + "/merchant/v1" + path
}
