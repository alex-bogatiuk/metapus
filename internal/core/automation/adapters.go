package automation

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
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

	// F-03: Validate URL to prevent SSRF to internal networks.
	if err := validateWebhookURL(urlRaw); err != nil {
		return fmt.Errorf("webhook url validation: %w", err)
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

	// F-10: Mandatory TLS for SMTP to prevent credential leakage via MITM.
	var auth smtp.Auth
	password := string(credentials)
	if password != "" {
		auth = smtp.PlainAuth("", from, password, smtpHost)
	}

	addr := net.JoinHostPort(smtpHost, smtpPort)

	// Use explicit TLS connection (port 465) or STARTTLS (port 587) with verification.
	tlsConfig := &tls.Config{ServerName: smtpHost}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, tlsConfig)
	if err != nil {
		// Fallback: try STARTTLS for port 587
		plainConn, dialErr := net.DialTimeout("tcp", addr, 10*time.Second)
		if dialErr != nil {
			return fmt.Errorf("smtp dial: %w", dialErr)
		}
		client, clientErr := smtp.NewClient(plainConn, smtpHost)
		if clientErr != nil {
			_ = plainConn.Close()
			return fmt.Errorf("smtp new client: %w", clientErr)
		}
		// Require STARTTLS — fail if server doesn't support it.
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return fmt.Errorf("smtp STARTTLS required but failed: %w", err)
		}
		if auth != nil {
			if err := client.Auth(auth); err != nil {
				_ = client.Close()
				return fmt.Errorf("smtp auth: %w", err)
			}
		}
		if err := client.Mail(from); err != nil {
			_ = client.Close()
			return fmt.Errorf("smtp mail: %w", err)
		}
		for _, rcpt := range recipients {
			if err := client.Rcpt(rcpt); err != nil {
				_ = client.Close()
				return fmt.Errorf("smtp rcpt: %w", err)
			}
		}
		wc, err := client.Data()
		if err != nil {
			_ = client.Close()
			return fmt.Errorf("smtp data: %w", err)
		}
		if _, err := wc.Write([]byte(msg)); err != nil {
			_ = wc.Close()
			_ = client.Close()
			return fmt.Errorf("smtp write: %w", err)
		}
		_ = wc.Close()
		return client.Quit()
	}

	// Direct TLS connection succeeded (port 465).
	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp new client (tls): %w", err)
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			_ = client.Close()
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		_ = client.Close()
		return fmt.Errorf("smtp mail: %w", err)
	}
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			_ = client.Close()
			return fmt.Errorf("smtp rcpt: %w", err)
		}
	}
	wc, err := client.Data()
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		_ = wc.Close()
		_ = client.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	_ = wc.Close()
	return client.Quit()
}

// validateWebhookURL prevents SSRF by rejecting URLs targeting internal networks.
// Checks: scheme must be http/https, host must not resolve to private/loopback/link-local IPs.
func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook url must use http or https scheme, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook url has empty host")
	}

	// Resolve DNS to check actual IPs (prevents DNS rebinding with static check).
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve webhook host %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("webhook url must not target internal network (resolved to %s)", ip)
		}
	}
	return nil
}
