// Package tron provides the TRON blockchain chain watcher.
// Monitors TRC-20 token transfers on the TRON network via TronGrid API.
package tron

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/pkg/logger"
)

const (
	_defaultTronGridURL = "https://api.trongrid.io"
	_trc20EventPath     = "/v1/contracts/%s/events"
	_blockPath          = "/wallet/getnowblock"
	_txInfoPath         = "/wallet/gettransactioninfobyid"
	_defaultTimeout     = 10 * time.Second
	_maxRetries         = 3
	_retryBaseDelay     = 500 * time.Millisecond
)

// ClientConfig holds TRON API client configuration.
type ClientConfig struct {
	BaseURL string // TronGrid API base URL
	APIKey  string // TronGrid API key (optional, increases rate limits)
}

// Client is an HTTP client for the TronGrid API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewClient creates a new TRON API client.
func NewClient(cfg ClientConfig) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = _defaultTronGridURL
	}

	return &Client{
		httpClient: &http.Client{Timeout: _defaultTimeout},
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
	}
}

// TRC20Event represents a TRC-20 Transfer event from TronGrid.
type TRC20Event struct {
	TransactionID string `json:"transaction_id"`
	BlockNumber   int64  `json:"block_number"`
	BlockTimestamp int64  `json:"block_timestamp"`
	Result        struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Value string `json:"value"`
	} `json:"result"`
	EventName string `json:"event_name"`
}

// TRC20EventResponse is the TronGrid API response for TRC-20 events.
type TRC20EventResponse struct {
	Data    []TRC20Event `json:"data"`
	Success bool         `json:"success"`
	Meta    struct {
		At          int64  `json:"at"`
		Fingerprint string `json:"fingerprint"`
		PageSize    int    `json:"page_size"`
	} `json:"meta"`
}

// BlockInfo represents the current TRON block.
type BlockInfo struct {
	BlockHeader struct {
		RawData struct {
			Number    int64 `json:"number"`
			Timestamp int64 `json:"timestamp"`
		} `json:"raw_data"`
	} `json:"block_header"`
}

// TransactionInfo holds confirmation data for a transaction.
type TransactionInfo struct {
	BlockNumber    int64 `json:"blockNumber"`
	BlockTimestamp int64 `json:"blockTimeStamp"`
}

// GetTRC20Events fetches TRC-20 Transfer events for a contract since a given timestamp.
func (c *Client) GetTRC20Events(ctx context.Context, contractAddress string, sinceTimestamp int64, fingerprint string) (*TRC20EventResponse, error) {
	url := fmt.Sprintf("%s"+_trc20EventPath+"?event_name=Transfer&min_block_timestamp=%d&order_by=block_timestamp,asc&limit=200",
		c.baseURL, contractAddress, sinceTimestamp)

	if fingerprint != "" {
		url += "&fingerprint=" + fingerprint
	}

	var resp TRC20EventResponse
	if err := c.doRequest(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("get TRC-20 events: %w", err)
	}

	return &resp, nil
}

// GetCurrentBlock returns the current block number.
func (c *Client) GetCurrentBlock(ctx context.Context) (int64, error) {
	url := c.baseURL + _blockPath

	var block BlockInfo
	if err := c.doRequest(ctx, url, &block); err != nil {
		return 0, fmt.Errorf("get current block: %w", err)
	}

	return block.BlockHeader.RawData.Number, nil
}

// GetTransactionInfo returns transaction info including block number.
func (c *Client) GetTransactionInfo(ctx context.Context, txHash string) (*TransactionInfo, error) {
	url := fmt.Sprintf("%s%s?value=%s", c.baseURL, _txInfoPath, txHash)

	var info TransactionInfo
	if err := c.doRequest(ctx, url, &info); err != nil {
		return nil, fmt.Errorf("get tx info: %w", err)
	}

	return &info, nil
}

// GetConfirmations returns the current confirmation count for a transaction.
func (c *Client) GetConfirmations(ctx context.Context, txHash string) (int, error) {
	info, err := c.GetTransactionInfo(ctx, txHash)
	if err != nil {
		return 0, err
	}

	currentBlock, err := c.GetCurrentBlock(ctx)
	if err != nil {
		return 0, err
	}

	if info.BlockNumber == 0 {
		return 0, nil // tx not yet in a block
	}

	confs := currentBlock - info.BlockNumber
	if confs < 0 {
		confs = 0
	}

	return int(confs), nil
}

// ToBlockchainEvent converts a TRC-20 event to a normalized BlockchainEvent.
// Addresses are converted from TronGrid hex format to TRON base58check.
func (c *Client) ToBlockchainEvent(event TRC20Event, networkID id.ID) crypto.BlockchainEvent {
	amount, _ := types.NewCryptoAmountFromString(event.Result.Value)

	return crypto.BlockchainEvent{
		Network:       "tron_mainnet",
		NetworkID:     networkID,
		TxHash:        event.TransactionID,
		FromAddress:   ConvertTronAddress(event.Result.From),
		ToAddress:     ConvertTronAddress(event.Result.To),
		Amount:        amount,
		BlockNumber:   event.BlockNumber,
		Confirmations: 0, // will be filled by watcher
		EventType:     crypto.EventTypeTransfer,
		Timestamp:     time.UnixMilli(event.BlockTimestamp),
	}
}

// doRequest performs an HTTP GET request with retry logic.
func (c *Client) doRequest(ctx context.Context, url string, target interface{}) error {
	var lastErr error

	for attempt := 0; attempt < _maxRetries; attempt++ {
		if attempt > 0 {
			delay := _retryBaseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}

		if c.apiKey != "" {
			req.Header.Set("TRON-PRO-API-KEY", c.apiKey)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			logger.Warn(ctx, "TronGrid request failed, retrying",
				"attempt", attempt+1,
				"error", err,
			)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("rate limited (429)")
			logger.Warn(ctx, "TronGrid rate limited, retrying",
				"attempt", attempt+1,
			)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
			continue
		}

		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}

		return nil
	}

	return fmt.Errorf("after %d retries: %w", _maxRetries, lastErr)
}

// ConvertTronAddress converts a hex TRON address to base58check format.
// TRON contract events return EVM hex addresses like "0xd83b630b...".
// TRON base58 addresses use prefix byte 0x41 (produces leading "T").
// Algorithm: strip 0x → prepend 41 → double-SHA256 → append 4-byte checksum → base58.
func ConvertTronAddress(hexAddr string) string {
	// Strip 0x prefix
	hexAddr = strings.TrimPrefix(hexAddr, "0x")
	if len(hexAddr) != 40 {
		return hexAddr // not a valid EVM address, return as-is
	}

	// Prepend TRON prefix byte (0x41)
	addrBytes, err := hex.DecodeString("41" + hexAddr)
	if err != nil {
		return hexAddr
	}

	// Double SHA-256 checksum
	h1 := sha256.Sum256(addrBytes)
	h2 := sha256.Sum256(h1[:])
	checksum := h2[:4]

	// Append checksum
	payload := append(addrBytes, checksum...)

	// Base58 encode
	return base58Encode(payload)
}

// _base58Alphabet is the Bitcoin/TRON base58 character set.
const _base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Encode encodes a byte slice to base58 string.
func base58Encode(input []byte) string {
	x := new(big.Int).SetBytes(input)
	base := big.NewInt(58)
	zero := big.NewInt(0)
	mod := new(big.Int)

	var result []byte
	for x.Cmp(zero) > 0 {
		x.DivMod(x, base, mod)
		result = append(result, _base58Alphabet[mod.Int64()])
	}

	// Preserve leading zero bytes as '1'
	for _, b := range input {
		if b != 0 {
			break
		}
		result = append(result, _base58Alphabet[0])
	}

	// Reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return string(result)
}

// ParseSunAmount converts a TronGrid amount string (in sun/wei) to CryptoAmount.
func ParseSunAmount(s string) (types.CryptoAmount, error) {
	return types.NewCryptoAmountFromString(s)
}

// FormatCryptoAmount formats a CryptoAmount to a human-readable string with the
// given number of decimal places. decimalPlaces MUST come from Token.DecimalPlaces
// metadata — never hardcode (§2.5).
func FormatCryptoAmount(amount types.CryptoAmount, decimalPlaces int) string {
	raw := amount.String()
	if decimalPlaces <= 0 {
		return raw
	}
	if len(raw) <= decimalPlaces {
		padding := strings.Repeat("0", decimalPlaces-len(raw))
		return "0." + padding + raw
	}
	return raw[:len(raw)-decimalPlaces] + "." + raw[len(raw)-decimalPlaces:]
}
