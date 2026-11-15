// Package tron provides the TRON blockchain chain watcher.
// Monitors TRC-20 token transfers on the TRON network via TronGrid API.
package tron

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/providerpolicy"
	"metapus/pkg/logger"
)

const (
	_defaultTronGridURL          = "https://api.trongrid.io"
	_trc20EventPath              = "/v1/contracts/%s/events"
	_accountTxPath               = "/v1/accounts/%s/transactions"
	_blockPath                   = "/wallet/getnowblock"
	_txInfoPath                  = "/wallet/gettransactioninfobyid"
	_solidityBlockPath           = "/walletsolidity/getnowblock"
	_solidityTxInfoPath          = "/walletsolidity/gettransactioninfobyid"
	_solidityAccountPath         = "/walletsolidity/getaccount"
	_solidityTriggerConstantPath = "/walletsolidity/triggerconstantcontract"
	_solidityBlockByNumPath      = "/walletsolidity/getblockbynum"
	_accountPath                 = "/wallet/getaccount"
	_accountResourcePath         = "/wallet/getaccountresource"
	_createTxPath                = "/wallet/" + "createtransaction"
	_createAccountPath           = "/wallet/createaccount"
	_triggerPath                 = "/wallet/triggersmartcontract"
	_triggerConstantPath         = "/wallet/triggerconstantcontract"
	_estimateEnergyPath          = "/wallet/estimateenergy"
	_broadcastPath               = "/wallet/broadcasttransaction"
	_defaultTimeout              = 10 * time.Second
	_maxRetries                  = 3
	_retryBaseDelay              = 500 * time.Millisecond
	_defaultFeeLimitSun          = 100_000_000
)

// ClientConfig holds TRON API client configuration.
type ClientConfig struct {
	WrapTransport func(http.RoundTripper) http.RoundTripper
	BaseURL       string // TronGrid API base URL
	APIKey        string // TronGrid API key (optional, increases rate limits)
	Timeout       time.Duration
	// DisableKeepAlives isolates providers that stop responding after a few
	// requests on the same persistent TLS connection.
	DisableKeepAlives bool
	// MaxRetries is the number of attempts against this one endpoint. Zero uses
	// the default; multi-provider pools set it to one and own failover.
	MaxRetries       int
	MaxResponseBytes int64
}

// Client is an HTTP client for the TronGrid API.
type Client struct {
	httpClient       *http.Client
	baseURL          string
	apiKey           string
	maxRetries       int
	maxResponseBytes int64
}

// HTTPStatusError preserves response classification for the endpoint circuit
// breaker without exposing request headers or secrets.
type HTTPStatusError struct {
	StatusCode int
	Body       string // Only a sanitized classification, never a response body.
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

func (e *HTTPStatusError) Transient() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// NewClient creates a new TRON API client.
func NewClient(cfg ClientConfig) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = _defaultTronGridURL
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = _defaultTimeout
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = _maxRetries
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = cfg.DisableKeepAlives
	var finalTransport http.RoundTripper = transport
	if cfg.WrapTransport != nil {
		finalTransport = cfg.WrapTransport(transport)
	}
	httpClient := &http.Client{Timeout: timeout, Transport: finalTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 10 << 20
	}
	return &Client{
		httpClient:       httpClient,
		baseURL:          strings.TrimRight(baseURL, "/"),
		apiKey:           cfg.APIKey,
		maxRetries:       maxRetries,
		maxResponseBytes: cfg.MaxResponseBytes,
	}
}

// AccountTransaction represents a native TRX transaction from TronGrid.
type AccountTransaction struct {
	TxID       string `json:"txID"`
	RawDataHex string `json:"raw_data_hex,omitempty"`
	Ret        []struct {
		ContractRet string `json:"contractRet"`
	} `json:"ret,omitempty"`
	BlockNumber    int64 `json:"blockNumber"`
	BlockTimestamp int64 `json:"block_timestamp"`
	RawData        struct {
		Contract []TransactionContract `json:"contract"`
	} `json:"raw_data"`
}

type TransactionContract struct {
	Type      string `json:"type"`
	Parameter struct {
		Value struct {
			OwnerAddress    string `json:"owner_address"`
			ToAddress       string `json:"to_address"`
			Amount          int64  `json:"amount"`
			ContractAddress string `json:"contract_address,omitempty"`
			CallValue       int64  `json:"call_value,omitempty"`
			Data            string `json:"data,omitempty"`
		} `json:"value"`
	} `json:"parameter"`
}

// BlockInfo represents the current TRON block.
type BlockInfo struct {
	BlockID      string               `json:"blockID"`
	Transactions []AccountTransaction `json:"transactions,omitempty"`
	BlockHeader  struct {
		RawData struct {
			Number     int64  `json:"number"`
			Timestamp  int64  `json:"timestamp"`
			ParentHash string `json:"parentHash,omitempty"`
		} `json:"raw_data"`
	} `json:"block_header"`
}

// TransactionLog is one TVM event emitted by a smart-contract transaction.
type TransactionLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

// TransactionInfo holds confirmation data for a transaction.
type TransactionInfo struct {
	ID                   string                `json:"id"`
	BlockHash            string                `json:"blockHash"`
	BlockNumber          int64                 `json:"blockNumber"`
	BlockTimestamp       int64                 `json:"blockTimeStamp"`
	Fee                  int64                 `json:"fee"`
	Log                  []TransactionLog      `json:"log"`
	Result               string                `json:"result,omitempty"`
	InternalTransactions []InternalTransaction `json:"internal_transactions,omitempty"`
	Receipt              struct {
		Result           string `json:"result"`
		EnergyUsageTotal int64  `json:"energy_usage_total"`
		EnergyFee        int64  `json:"energy_fee"`
		NetFee           int64  `json:"net_fee"`
		NetUsage         int64  `json:"net_usage"`
	} `json:"receipt"`
}

type InternalTransaction struct {
	Hash              string `json:"hash"`
	CallerAddress     string `json:"caller_address"`
	TransferToAddress string `json:"transferTo_address"`
	Rejected          bool   `json:"rejected,omitempty"`
	Note              string `json:"note,omitempty"`
	CallValueInfo     []struct {
		CallValue int64  `json:"callValue"`
		TokenID   string `json:"tokenId,omitempty"`
	} `json:"callValueInfo"`
}

// GetBlockReceipts uses node APIs, never the account/events indexer.
func (c *Client) GetBlockReceipts(ctx context.Context, height int64, solidified bool) ([]TransactionInfo, error) {
	path := "/wallet/gettransactioninfobyblocknum"
	if solidified {
		path = "/walletsolidity/gettransactioninfobyblocknum"
	}
	var receipts []TransactionInfo
	if err := c.doPost(ctx, path, map[string]int64{"num": height}, &receipts); err != nil {
		return nil, err
	}
	return receipts, nil
}

// EnergyEstimate is the node estimate for one smart-contract execution.
type EnergyEstimate struct {
	EnergyRequired int64
}

// UnsignedTransaction is the TRON node response for a transaction prepared for signing.
type UnsignedTransaction struct {
	TxID       string          `json:"txID"`
	RawDataHex string          `json:"raw_data_hex"`
	RawData    json.RawMessage `json:"raw_data"`
	Visible    bool            `json:"visible,omitzero"`
}

// SignedTransaction is the broadcast payload expected by TRON nodes.
type SignedTransaction struct {
	TxID       string          `json:"txID"`
	RawDataHex string          `json:"raw_data_hex"`
	RawData    json.RawMessage `json:"raw_data"`
	Signature  []string        `json:"signature"`
	Visible    bool            `json:"visible,omitzero"`
}

// TriggerContractParams describes a TRC20 transfer transaction.
type TriggerContractParams struct {
	OwnerAddress    string
	ContractAddress string
	ToAddress       string
	Amount          *big.Int
	FeeLimit        int64
}

// BroadcastResult is the TRON broadcast response.
type BroadcastResult struct {
	Result  bool   `json:"result"`
	TxID    string `json:"txid"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// ConstantContractResponse is the response for read-only smart-contract calls.
type ConstantContractResponse struct {
	ConstantResult []string `json:"constant_result"`
	Result         struct {
		Result  bool   `json:"result"`
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"result"`
}

// AccountResponse is the node response for a TRON account.
type AccountResponse struct {
	Address    string `json:"address"`
	CreateTime int64  `json:"create_time"`
	Balance    int64  `json:"balance"`
}

// AccountResourceResponse is the node response for delegated/free account resources.
type AccountResourceResponse struct {
	FreeNetUsed       int64 `json:"freeNetUsed"`
	FreeNetLimit      int64 `json:"freeNetLimit"`
	EnergyUsed        int64 `json:"EnergyUsed"`
	EnergyLimit       int64 `json:"EnergyLimit"`
	TotalEnergyLimit  int64 `json:"TotalEnergyLimit"`
	TotalEnergyWeight int64 `json:"TotalEnergyWeight"`
}

// CreateTransferTransaction prepares a native TRX transfer transaction.
func (c *Client) CreateTransferTransaction(ctx context.Context, from, to string, amountSun int64) (*UnsignedTransaction, error) {
	body := map[string]any{
		"owner_address": from,
		"to_address":    to,
		"amount":        amountSun,
		"visible":       true,
	}

	var tx UnsignedTransaction
	if err := c.doPost(ctx, _createTxPath, body, &tx); err != nil {
		return nil, fmt.Errorf("create transfer transaction: %w", err)
	}
	tx.Visible = true
	return &tx, nil
}

// CreateAccountTransaction prepares a TRON account activation transaction.
func (c *Client) CreateAccountTransaction(ctx context.Context, ownerAddress, accountAddress string) (*UnsignedTransaction, error) {
	body := map[string]any{
		"owner_address":   ownerAddress,
		"account_address": accountAddress,
		"visible":         true,
	}

	var tx UnsignedTransaction
	if err := c.doPost(ctx, _createAccountPath, body, &tx); err != nil {
		return nil, fmt.Errorf("create account transaction: %w", err)
	}
	tx.Visible = true
	return &tx, nil
}

// TriggerSmartContract prepares a TRC20 transfer transaction.
func (c *Client) TriggerSmartContract(ctx context.Context, params TriggerContractParams) (*UnsignedTransaction, error) {
	if params.Amount == nil || params.Amount.Sign() < 0 {
		return nil, fmt.Errorf("amount must be non-negative")
	}

	parameter, err := abiEncodeTransfer(params.ToAddress, params.Amount)
	if err != nil {
		return nil, err
	}

	feeLimit := params.FeeLimit
	if feeLimit == 0 {
		feeLimit = _defaultFeeLimitSun
	}

	body := map[string]any{
		"owner_address":     params.OwnerAddress,
		"contract_address":  params.ContractAddress,
		"function_selector": "transfer(address,uint256)",
		"parameter":         parameter,
		"fee_limit":         feeLimit,
		"visible":           true,
	}

	var resp struct {
		Transaction UnsignedTransaction `json:"transaction"`
	}
	if err := c.doPost(ctx, _triggerPath, body, &resp); err != nil {
		return nil, fmt.Errorf("trigger smart contract: %w", err)
	}
	resp.Transaction.Visible = true
	return &resp.Transaction, nil
}

// EstimateEnergy estimates resource demand for the same TRC-20 call that will
// later be prepared for signing. Callers fail closed when the node cannot
// provide an estimate, because silent TRX burn is not an acceptable fallback.
func (c *Client) EstimateEnergy(ctx context.Context, params TriggerContractParams) (EnergyEstimate, error) {
	if params.Amount == nil || params.Amount.Sign() < 0 {
		return EnergyEstimate{}, fmt.Errorf("amount must be non-negative")
	}
	parameter, err := abiEncodeTransfer(params.ToAddress, params.Amount)
	if err != nil {
		return EnergyEstimate{}, err
	}
	body := map[string]any{
		"owner_address":     params.OwnerAddress,
		"contract_address":  params.ContractAddress,
		"function_selector": "transfer(address,uint256)",
		"parameter":         parameter,
		"visible":           true,
	}
	var resp struct {
		Result struct {
			Result  bool   `json:"result"`
			Code    string `json:"code,omitempty"`
			Message string `json:"message,omitempty"`
		} `json:"result"`
		EnergyRequired int64 `json:"energy_required"`
	}
	if err := c.doPost(ctx, _estimateEnergyPath, body, &resp); err != nil {
		return EnergyEstimate{}, fmt.Errorf("estimate smart contract energy: %w", err)
	}
	if !resp.Result.Result || resp.EnergyRequired <= 0 {
		return EnergyEstimate{}, fmt.Errorf("energy estimate rejected: code=%s message=%s energy=%d",
			resp.Result.Code, decodeHexMessage(resp.Result.Message), resp.EnergyRequired)
	}
	return EnergyEstimate{EnergyRequired: resp.EnergyRequired}, nil
}

// GetTRC20Balance returns balanceOf(owner) for a TRC-20 contract.
func (c *Client) GetTRC20Balance(ctx context.Context, ownerAddress, contractAddress string) (types.CryptoAmount, error) {
	return c.GetTRC20BalanceFor(ctx, ownerAddress, ownerAddress, contractAddress)
}

// GetTRC20BalanceFor returns balanceOf(accountAddress), using callerAddress as
// the constant-contract owner. This permits checking an inactive pool address
// while the caller is an already-active platform hot wallet.
func (c *Client) GetTRC20BalanceFor(ctx context.Context, callerAddress, accountAddress, contractAddress string) (types.CryptoAmount, error) {
	return c.getTRC20BalanceForPath(ctx, _triggerConstantPath, callerAddress, accountAddress, contractAddress)
}

func (c *Client) GetSolidifiedTRC20Balance(ctx context.Context, ownerAddress, contractAddress string) (types.CryptoAmount, error) {
	return c.getTRC20BalanceForPath(ctx, _solidityTriggerConstantPath, ownerAddress, ownerAddress, contractAddress)
}

func (c *Client) getTRC20BalanceForPath(ctx context.Context, path, callerAddress, accountAddress, contractAddress string) (types.CryptoAmount, error) {
	parameter, err := abiEncodeAddress(accountAddress)
	if err != nil {
		return types.ZeroCryptoAmount(), err
	}

	body := map[string]any{
		"owner_address":     callerAddress,
		"contract_address":  contractAddress,
		"function_selector": "balanceOf(address)",
		"parameter":         parameter,
		"visible":           true,
	}

	var resp ConstantContractResponse
	if err := c.doPost(ctx, path, body, &resp); err != nil {
		return types.ZeroCryptoAmount(), fmt.Errorf("trigger constant contract: %w", err)
	}
	if !resp.Result.Result {
		return types.ZeroCryptoAmount(), fmt.Errorf("constant contract rejected: code=%s message=%s", resp.Result.Code, decodeHexMessage(resp.Result.Message))
	}
	if len(resp.ConstantResult) == 0 {
		return types.ZeroCryptoAmount(), fmt.Errorf("constant contract returned empty result")
	}
	value := new(big.Int)
	if _, ok := value.SetString(strings.TrimPrefix(resp.ConstantResult[0], "0x"), 16); !ok {
		return types.ZeroCryptoAmount(), fmt.Errorf("invalid balanceOf result: %s", resp.ConstantResult[0])
	}
	amount, err := types.NewCryptoAmountFromBigInt(value)
	if err != nil {
		return types.ZeroCryptoAmount(), fmt.Errorf("invalid balanceOf amount: %w", err)
	}
	return amount, nil
}

// GetTRXBalance returns the native TRX balance in SUN for an account.
func (c *Client) GetTRXBalance(ctx context.Context, address string) (types.CryptoAmount, error) {
	resp, err := c.GetAccount(ctx, address)
	if err != nil {
		return types.ZeroCryptoAmount(), fmt.Errorf("get TRX account balance: %w", err)
	}
	if resp.Balance < 0 {
		return types.ZeroCryptoAmount(), fmt.Errorf("TRX account balance is negative")
	}
	return types.NewCryptoAmountFromInt64(resp.Balance), nil
}

func (c *Client) GetSolidifiedTRXBalance(ctx context.Context, address string) (types.CryptoAmount, error) {
	resp, err := c.getAccountAtPath(ctx, _solidityAccountPath, address)
	if err != nil {
		return types.ZeroCryptoAmount(), fmt.Errorf("get solidified TRX account balance: %w", err)
	}
	if resp.Balance < 0 {
		return types.ZeroCryptoAmount(), fmt.Errorf("solidified TRX account balance is negative")
	}
	return types.NewCryptoAmountFromInt64(resp.Balance), nil
}

// GetAccount returns TRON account state. Inactive accounts usually return an empty object.
func (c *Client) GetAccount(ctx context.Context, address string) (*AccountResponse, error) {
	return c.getAccountAtPath(ctx, _accountPath, address)
}

func (c *Client) GetSolidifiedAccount(ctx context.Context, address string) (*AccountResponse, error) {
	return c.getAccountAtPath(ctx, _solidityAccountPath, address)
}

func (c *Client) getAccountAtPath(ctx context.Context, path, address string) (*AccountResponse, error) {
	body := map[string]any{
		"address": address,
		"visible": true,
	}

	var resp AccountResponse
	if err := c.doPost(ctx, path, body, &resp); err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	return &resp, nil
}

// AccountExists returns true when the TRON account exists in chain state.
func (c *Client) AccountExists(ctx context.Context, address string) (bool, error) {
	resp, err := c.GetAccount(ctx, address)
	if err != nil {
		return false, err
	}
	return resp != nil && (strings.TrimSpace(resp.Address) != "" || resp.CreateTime > 0 || resp.Balance > 0), nil
}

// GetAccountResource returns TRON resource limits for an account.
func (c *Client) GetAccountResource(ctx context.Context, address string) (*AccountResourceResponse, error) {
	body := map[string]any{
		"address": address,
		"visible": true,
	}

	var resp AccountResourceResponse
	if err := c.doPost(ctx, _accountResourcePath, body, &resp); err != nil {
		return nil, fmt.Errorf("get account resource: %w", err)
	}
	return &resp, nil
}

// BroadcastTransaction broadcasts a signed TRON transaction.
func (c *Client) BroadcastTransaction(ctx context.Context, tx SignedTransaction) (*BroadcastResult, error) {
	if err := validateBroadcastTransaction(tx); err != nil {
		return nil, err
	}
	var result BroadcastResult
	if err := c.doPost(ctx, _broadcastPath, tx, &result); err != nil {
		return nil, fmt.Errorf("broadcast transaction: %w", err)
	}
	return &result, nil
}

func validateBroadcastTransaction(tx SignedTransaction) error {
	if strings.TrimSpace(tx.TxID) == "" {
		return fmt.Errorf("broadcast transaction txID is required")
	}
	if strings.TrimSpace(tx.RawDataHex) == "" {
		return fmt.Errorf("broadcast transaction raw_data_hex is required")
	}
	rawData := bytes.TrimSpace(tx.RawData)
	if len(rawData) == 0 || bytes.Equal(rawData, []byte("null")) {
		return fmt.Errorf("broadcast transaction raw_data is required")
	}
	if !json.Valid(tx.RawData) {
		return fmt.Errorf("broadcast transaction raw_data must be valid JSON")
	}
	if rawData[0] != '{' {
		return fmt.Errorf("broadcast transaction raw_data must be a JSON object")
	}
	if !tx.Visible {
		return fmt.Errorf("broadcast transaction visible=true is required")
	}
	if len(tx.Signature) == 0 {
		return fmt.Errorf("broadcast transaction signature is required")
	}
	for _, sig := range tx.Signature {
		if strings.TrimSpace(sig) == "" {
			return fmt.Errorf("broadcast transaction signature is required")
		}
	}
	return nil
}

// GetCurrentBlock returns the current block number.
func (c *Client) GetCurrentBlock(ctx context.Context) (int64, error) {
	block, err := c.GetCurrentBlockInfo(ctx)
	if err != nil {
		return 0, err
	}
	return block.BlockHeader.RawData.Number, nil
}

func (c *Client) GetCurrentBlockInfo(ctx context.Context) (*BlockInfo, error) {
	return c.getCurrentBlockInfoAtPath(ctx, _blockPath)
}

func (c *Client) GetSolidifiedCurrentBlockInfo(ctx context.Context) (*BlockInfo, error) {
	return c.getCurrentBlockInfoAtPath(ctx, _solidityBlockPath)
}

func (c *Client) GetSolidifiedBlockByNumber(ctx context.Context, number int64) (*BlockInfo, error) {
	if number < 0 {
		return nil, fmt.Errorf("block number must be non-negative")
	}
	var block BlockInfo
	if err := c.doPost(ctx, _solidityBlockByNumPath, map[string]int64{"num": number}, &block); err != nil {
		return nil, fmt.Errorf("get solidified block by number: %w", err)
	}
	return &block, nil
}

func (c *Client) getCurrentBlockInfoAtPath(ctx context.Context, path string) (*BlockInfo, error) {
	url := c.baseURL + path

	var block BlockInfo
	if err := c.doRequest(ctx, url, &block); err != nil {
		return nil, fmt.Errorf("get current block: %w", err)
	}
	return &block, nil
}

// GetTransactionInfo returns transaction info including block number.
func (c *Client) GetTransactionInfo(ctx context.Context, txHash string) (*TransactionInfo, error) {
	return c.getTransactionInfoAtPath(ctx, _txInfoPath, txHash)
}

func (c *Client) GetSolidifiedTransactionInfo(ctx context.Context, txHash string) (*TransactionInfo, error) {
	return c.getTransactionInfoAtPath(ctx, _solidityTxInfoPath, txHash)
}

func (c *Client) getTransactionInfoAtPath(ctx context.Context, path, txHash string) (*TransactionInfo, error) {
	var info TransactionInfo
	if err := c.doPost(ctx, path, map[string]string{"value": txHash}, &info); err != nil {
		return nil, fmt.Errorf("get tx info: %w", err)
	}

	return &info, nil
}

// doRequest performs an HTTP GET request with retry logic.
func (c *Client) doRequest(ctx context.Context, url string, target any) error {
	return c.doHTTP(ctx, http.MethodGet, url, nil, target)
}

// doPost performs an HTTP POST request with retry logic.
func (c *Client) doPost(ctx context.Context, path string, body any, target any) error {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	return c.doHTTP(ctx, http.MethodPost, c.baseURL+path, reqBody, target)
}

// doHTTP performs an HTTP request with retry logic.
func (c *Client) doHTTP(ctx context.Context, method, url string, reqBody []byte, target any) error {
	var lastErr error

	for attempt := range c.maxRetries {
		if attempt > 0 {
			delay := _retryBaseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		var bodyReader io.Reader
		if reqBody != nil {
			bodyReader = bytes.NewReader(reqBody)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}

		if c.apiKey != "" {
			req.Header.Set("TRON-PRO-API-KEY", c.apiKey)
		}
		req.Header.Set("Accept", "application/json")
		if reqBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			logger.Warn(ctx, "TronGrid request failed, retrying",
				"attempt", attempt+1,
				"error_category", sanitizeRPCError(err),
			)
			continue
		}

		if err := providerpolicy.LimitResponse(resp, c.maxResponseBytes, nil); err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			httpErr := &HTTPStatusError{StatusCode: resp.StatusCode, Body: "provider response"}
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return httpErr
			}
			lastErr = httpErr
			continue
		}

		if len(bytes.TrimSpace(body)) == 0 {
			return providerpolicy.ErrInvalidResponse
		}
		if bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
			var envelope struct {
				Error string `json:"Error"`
			}
			if json.Unmarshal(body, &envelope) == nil && envelope.Error != "" {
				message := strings.ToLower(envelope.Error)
				if strings.Contains(message, "not supported") || strings.Contains(message, "not support") || strings.Contains(message, "unsupported") || strings.Contains(message, "method not found") {
					return providerpolicy.ErrUnsupported
				}
				return providerpolicy.ErrInvalidResponse
			}
		}
		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}

		return nil
	}

	return fmt.Errorf("after %d retries: %w", c.maxRetries, lastErr)
}

// abiEncodeTransfer ABI-encodes transfer(address,uint256) parameters.
func abiEncodeTransfer(toAddr string, amount *big.Int) (string, error) {
	addrBytes, err := tronAddressToABIBytes(toAddr)
	if err != nil {
		return "", err
	}

	if amount == nil || amount.Sign() < 0 {
		return "", fmt.Errorf("amount must be non-negative")
	}
	amountBytes := amount.Bytes()
	if len(amountBytes) > 32 {
		return "", fmt.Errorf("amount exceeds uint256")
	}

	addressWord := make([]byte, 32)
	copy(addressWord[12:], addrBytes)

	amountWord := make([]byte, 32)
	copy(amountWord[32-len(amountBytes):], amountBytes)

	return hex.EncodeToString(append(addressWord, amountWord...)), nil
}

func abiEncodeAddress(addr string) (string, error) {
	addrBytes, err := tronAddressToABIBytes(addr)
	if err != nil {
		return "", err
	}
	addressWord := make([]byte, 32)
	copy(addressWord[12:], addrBytes)
	return hex.EncodeToString(addressWord), nil
}

func tronAddressToABIBytes(addr string) ([]byte, error) {
	addr = strings.TrimPrefix(addr, "0x")

	if len(addr) == 42 && strings.HasPrefix(strings.ToLower(addr), "41") {
		decoded, err := hex.DecodeString(addr[2:])
		if err != nil {
			return nil, fmt.Errorf("decode tron hex address: %w", err)
		}
		return decoded, nil
	}

	if len(addr) == 40 {
		decoded, err := hex.DecodeString(addr)
		if err != nil {
			return nil, fmt.Errorf("decode hex address: %w", err)
		}
		return decoded, nil
	}

	decoded, err := base58Decode(addr)
	if err != nil {
		return nil, fmt.Errorf("decode base58 address: %w", err)
	}
	if len(decoded) != 25 || decoded[0] != 0x41 {
		return nil, fmt.Errorf("invalid TRON address")
	}

	payload := decoded[:21]
	checksum := decoded[21:]
	h1 := sha256.Sum256(payload)
	h2 := sha256.Sum256(h1[:])
	if !bytes.Equal(checksum, h2[:4]) {
		return nil, fmt.Errorf("invalid TRON address checksum")
	}

	return payload[1:], nil
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

// NormalizeTronAddress converts common TRON address encodings to base58check.
func NormalizeTronAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	trimmed := strings.TrimPrefix(addr, "0x")
	lower := strings.ToLower(trimmed)
	if len(trimmed) == 42 && strings.HasPrefix(lower, "41") {
		return ConvertTronAddress(trimmed[2:])
	}
	if len(trimmed) == 40 {
		return ConvertTronAddress(trimmed)
	}
	return addr
}

func ValidateTronAddress(addr string) error {
	_, err := tronAddressToABIBytes(addr)
	return err
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

func base58Decode(input string) ([]byte, error) {
	result := big.NewInt(0)
	base := big.NewInt(58)

	for _, r := range []byte(input) {
		idx := strings.IndexByte(_base58Alphabet, r)
		if idx < 0 {
			return nil, fmt.Errorf("invalid base58 character: %q", r)
		}
		result.Mul(result, base)
		result.Add(result, big.NewInt(int64(idx)))
	}

	decoded := result.Bytes()
	for i := 0; i < len(input) && input[i] == _base58Alphabet[0]; i++ {
		decoded = append([]byte{0}, decoded...)
	}
	return decoded, nil
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

func decodeHexMessage(message string) string {
	if message == "" {
		return ""
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(message, "0x"))
	if err != nil {
		return message
	}
	return string(decoded)
}

func (c *Client) GetBlockByNumber(ctx context.Context, number int64) (*BlockInfo, error) {
	if number < 0 {
		return nil, errors.New("negative TRON block number")
	}
	var block BlockInfo
	if err := c.doPost(ctx, "/wallet/getblockbynum", map[string]int64{"num": number}, &block); err != nil {
		return nil, err
	}
	return &block, nil
}
