package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"metapus/pkg/logger"
)

// WebhookAdapter sends an HTTP POST request with the rendered payload.
type WebhookAdapter struct {
	client *http.Client
}

// NewWebhookAdapter creates a new Webhook adapter.
func NewWebhookAdapter() *WebhookAdapter {
	return &WebhookAdapter{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Deliver sends the payload to the webhook URL from the channel destination.
func (a *WebhookAdapter) Deliver(ctx context.Context, destination map[string]any, accountConfig map[string]any, credentials []byte, payload string) error {
	// URL comes from Channel.Destination
	urlRaw, ok := destination["url"].(string)
	if !ok || urlRaw == "" {
		return fmt.Errorf("missing 'url' in channel destination")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlRaw, bytes.NewBufferString(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Metapus-AutomationEngine/2.0")

	// Credentials: Bearer token or API secret
	if secret := string(credentials); secret != "" {
		authType, _ := accountConfig["auth_type"].(string)
		switch authType {
		case "header":
			headerName, _ := accountConfig["header_name"].(string)
			if headerName == "" {
				headerName = "X-Webhook-Secret"
			}
			req.Header.Set(headerName, secret)
		default:
			req.Header.Set("Authorization", "Bearer "+secret)
		}
	}

	// Custom headers from account config
	if headers, ok := accountConfig["headers"].(map[string]any); ok {
		for k, v := range headers {
			if sv, ok := v.(string); ok {
				req.Header.Set(k, sv)
			}
		}
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// TelegramAdapter sends a message to a Telegram chat via Bot API.
type TelegramAdapter struct {
	client *http.Client
}

// NewTelegramAdapter creates a new Telegram adapter.
func NewTelegramAdapter() *TelegramAdapter {
	return &TelegramAdapter{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Deliver sends the message to a Telegram chat.
// destination["chat_id"]: the target chat (from Channel)
// credentials: Bot Token (from Account)
// accountConfig["parse_mode"]: optional (Markdown, HTML, MarkdownV2)
func (a *TelegramAdapter) Deliver(ctx context.Context, destination map[string]any, accountConfig map[string]any, credentials []byte, payload string) error {
	chatID := destination["chat_id"]
	if chatID == nil {
		return fmt.Errorf("missing 'chat_id' in channel destination")
	}

	botToken := string(credentials)
	if botToken == "" {
		return fmt.Errorf("missing bot token in account credentials")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	bodyMap := map[string]interface{}{
		"chat_id": chatID,
		"text":    payload,
	}
	if pm, ok := accountConfig["parse_mode"].(string); ok && pm != "" {
		bodyMap["parse_mode"] = pm
	}
	// Allow disabling link previews
	if disable, ok := accountConfig["disable_web_page_preview"].(bool); ok && disable {
		bodyMap["disable_web_page_preview"] = true
	}

	reqBody, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.client.Do(req)
		if err != nil {
			return fmt.Errorf("do request: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			var errResp struct {
				Parameters struct {
					RetryAfter int `json:"retry_after"`
				} `json:"parameters"`
			}
			if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Parameters.RetryAfter > 0 {
				if attempt < maxAttempts {
					logger.Warn(ctx, "telegram rate limit hit, sleeping", "retry_after", errResp.Parameters.RetryAfter, "attempt", attempt)
					
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(time.Duration(errResp.Parameters.RetryAfter) * time.Second):
					}
					
					continue
				}
			}
		}

		logger.Error(ctx, "telegram API error", "status", resp.StatusCode, "response", string(respBody))
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return fmt.Errorf("telegram API failed after %d attempts", maxAttempts)
}

// EmailAdapter sends emails via net/smtp.
type EmailAdapter struct{}

// NewEmailAdapter creates a new Email adapter.
func NewEmailAdapter() *EmailAdapter {
	return &EmailAdapter{}
}

// Deliver sends an email message.
// accountConfig: {"smtp_host": "...", "smtp_port": "587", "from": "noreply@example.com"}
// credentials: SMTP password
// destination: {"to": "user@example.com"} or {"to": ["a@x.com", "b@x.com"]}
// payload: email body (subject extracted via --- separator or from first line)
func (a *EmailAdapter) Deliver(ctx context.Context, destination map[string]any, accountConfig map[string]any, credentials []byte, payload string) error {
	smtpHost, _ := accountConfig["smtp_host"].(string)
	smtpPort, _ := accountConfig["smtp_port"].(string)
	from, _ := accountConfig["from"].(string)

	if smtpHost == "" || from == "" {
		return fmt.Errorf("missing smtp_host or from in account config")
	}
	if smtpPort == "" {
		smtpPort = "587"
	}

	// Parse recipients
	var recipients []string
	switch to := destination["to"].(type) {
	case string:
		recipients = []string{to}
	case []interface{}:
		for _, v := range to {
			if s, ok := v.(string); ok {
				recipients = append(recipients, s)
			}
		}
	case []string:
		recipients = to
	default:
		return fmt.Errorf("missing or invalid 'to' in channel destination")
	}

	if len(recipients) == 0 {
		return fmt.Errorf("no recipients specified")
	}

	// Parse subject from payload: first line = subject, rest = body
	subject := "Notification"
	body := payload
	if idx := strings.Index(payload, "\n"); idx > 0 {
		subject = strings.TrimSpace(payload[:idx])
		body = strings.TrimSpace(payload[idx+1:])
	}

	// Build email message
	contentType, _ := accountConfig["content_type"].(string)
	if contentType == "" {
		contentType = "text/plain"
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s; charset=UTF-8\r\n\r\n%s",
		from,
		strings.Join(recipients, ", "),
		subject,
		contentType,
		body,
	)

	// SMTP auth
	var auth smtp.Auth
	password := string(credentials)
	if password != "" {
		auth = smtp.PlainAuth("", from, password, smtpHost)
	}

	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	if err := smtp.SendMail(addr, auth, from, recipients, []byte(msg)); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}

	return nil
}
