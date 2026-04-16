package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
			Timeout: 10 * time.Second, // Prevent hanging requests
		},
	}
}

// Execute performs the HTTP request.
func (a *WebhookAdapter) Execute(ctx context.Context, config map[string]interface{}, credentials []byte, payload string) error {
	urlRaw, ok := config["url"].(string)
	if !ok || urlRaw == "" {
		return fmt.Errorf("missing or invalid 'url' in webhook config")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlRaw, bytes.NewBufferString(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Default headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Metapus-AutomationEngine/1.0")

	// If credentials exist, assume it's a Bearer token or Secret
	secret := string(credentials)
	if secret != "" {
		// As a simple example, we use Bearer. For advanced webhooks like HMAC, 
		// we could check config["auth_type"] = "hmac_sha256" and generate signature header.
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// TelegramAdapter sends a message to a telegram chat.
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

// Execute sends the message to the Telegram Bot API.
func (a *TelegramAdapter) Execute(ctx context.Context, config map[string]interface{}, credentials []byte, payload string) error {
	chatID, ok := config["chat_id"]
	if !ok {
		return fmt.Errorf("missing 'chat_id' in telegram config")
	}

	botToken := string(credentials)
	if botToken == "" {
		return fmt.Errorf("missing bot token in telegram credentials")
	}

	// Payload contains the rendered message text
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	// Format request body
	bodyMap := map[string]interface{}{
		"chat_id": chatID,
		"text":    payload, // Assuming template renders pure text or markup
	}
	// Support optional parse_mode
	if pm, ok := config["parse_mode"].(string); ok && pm != "" {
		bodyMap["parse_mode"] = pm
	}

	// If the template wanted to emit JSON natively, we could just pass payload,
	// but normally for Telegram people write text templates and we form the JSON wrapper here.
	
	// Check if the payload is ALREADY JSON containing chat_id/text.
	// This happens if the user used `{{ json . }}` to make a full tg payload.
	// We'll assume the user typed pure text in the template, so we wrap it.
	
	// Wrap
	reqBody, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		logger.Error(ctx, "telegram API error", "status", resp.StatusCode, "response", string(respBody))
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
