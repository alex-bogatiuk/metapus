// Package getblocknet implements the paid-submit / free-poll Getblock.net AML
// protocol. It deliberately does not use the auto-failover RPC transport.
package getblocknet

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
)

const (
	ProviderName = "getblock_net"
	_endpoint    = "https://api.getblock.net/rpc/v1/request"
	_maxBody     = 2 << 20
	_adapter     = "getblock-net.v1"
)

type AssetMapping struct {
	NetworkID          id.ID
	TokenID            id.ID
	ContractAddress    string
	Currency           string
	ProviderTokenID    string
	Version            string
	Native             bool
	CapabilityAccepted bool // real contract fixtures must precede enablement
}

type Config struct {
	AccountID       id.ID
	BearerToken     string
	ContractVersion string
	Mappings        []AssetMapping
	PendingStatuses []string // only statuses confirmed by the accepted contract
	FailureStatuses []string
}

type Client struct {
	config Config
	http   *http.Client
	now    func() time.Time
}

// ProtocolError contains safe diagnostics only. Upstream messages/bodies can
// contain subjects or credentials and are never incorporated into errors.
type ProtocolError struct {
	Kind               string
	HTTPStatus         int
	RPCCode            int
	SubmissionMayExist bool
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("getblock.net AML %s (HTTP %d, RPC %d)", e.Kind, e.HTTPStatus, e.RPCCode)
}

func (e *ProtocolError) OutcomeUnknown() bool { return e.SubmissionMayExist }

func (e *ProtocolError) Temporary() bool {
	return e.Kind == "transport" || e.Kind == "response_body" || e.HTTPStatus == http.StatusTooManyRequests || e.HTTPStatus >= 500
}

func New(config Config, client *http.Client) (*Client, error) {
	if id.IsNil(config.AccountID) || strings.TrimSpace(config.BearerToken) == "" || strings.ContainsAny(config.BearerToken, "\r\n") || config.ContractVersion == "" {
		return nil, &ProtocolError{Kind: "configuration"}
	}
	config.Mappings = slices.Clone(config.Mappings)
	config.PendingStatuses = slices.Clone(config.PendingStatuses)
	config.FailureStatuses = slices.Clone(config.FailureStatuses)
	seen := make(map[[2]id.ID]bool, len(config.Mappings))
	for _, mapping := range config.Mappings {
		key := [2]id.ID{mapping.NetworkID, mapping.TokenID}
		if id.IsNil(mapping.NetworkID) || id.IsNil(mapping.TokenID) || mapping.Version == "" || seen[key] ||
			(mapping.Currency != "TRX" && mapping.Currency != "ETH") ||
			(mapping.Native && (mapping.ContractAddress != "" || mapping.ProviderTokenID != "")) ||
			(!mapping.Native && (mapping.ContractAddress == "" || mapping.ProviderTokenID == "")) {
			return nil, &ProtocolError{Kind: "asset_mapping"}
		}
		seen[key] = true
	}
	for _, status := range config.PendingStatuses {
		if status == "" || status == "SUCCESS" || slices.Contains(config.FailureStatuses, status) {
			return nil, &ProtocolError{Kind: "status_mapping"}
		}
	}
	for _, status := range config.FailureStatuses {
		if status == "" || status == "SUCCESS" {
			return nil, &ProtocolError{Kind: "status_mapping"}
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	copyClient := *client
	if copyClient.Timeout <= 0 {
		copyClient.Timeout = 5 * time.Second
	}
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{config: config, http: &copyClient, now: time.Now}, nil
}

func (c *Client) Name() string { return ProviderName }

func (c *Client) AccountID() id.ID { return c.config.AccountID }

func (c *Client) mapping(request aml.CheckRequest) (AssetMapping, error) {
	if err := request.Validate(); err != nil {
		return AssetMapping{}, err
	}
	for _, mapping := range c.config.Mappings {
		if mapping.NetworkID == request.NetworkID && mapping.TokenID == request.TokenID && mapping.CapabilityAccepted && equalAddress(mapping.Currency, mapping.ContractAddress, request.ContractAddress) {
			return mapping, nil
		}
	}
	return AssetMapping{}, &ProtocolError{Kind: "unsupported_asset"}
}

type checkParams struct {
	Address  string `json:"addr"`
	Currency string `json:"currency"`
	TokenID  string `json:"token_id,omitempty"`
	Tx       string `json:"tx,omitempty"`
}

type vendorCheck struct {
	Hash        string          `json:"hash"`
	Currency    string          `json:"currency"`
	Address     string          `json:"address"`
	Transaction *string         `json:"transaction"`
	TokenID     json.RawMessage `json:"token_id"`
	Status      string          `json:"status"`
	ResultDate  string          `json:"resultDate"`
	Report      *vendorReport   `json:"report"`
}

type checkResponse struct {
	Check vendorCheck `json:"check"`
}

func (c *Client) Submit(ctx context.Context, request aml.CheckRequest, requestID string) (aml.Ticket, error) {
	mapping, err := c.mapping(request)
	if err != nil {
		return aml.Ticket{}, err
	}
	method := "checkup.checkaddr"
	if request.Kind == aml.TransferSubject {
		method = "checkup.checktx"
	}
	result, _, err := call(c, ctx, method, requestID, checkParams{Address: request.Address, Currency: mapping.Currency, TokenID: mapping.ProviderTokenID, Tx: request.TxHash}, true)
	if err != nil {
		return aml.Ticket{}, err
	}
	var response checkResponse
	if err := json.Unmarshal(result, &response); err != nil || response.Check.Hash == "" ||
		(response.Check.Currency != "" && !strings.EqualFold(response.Check.Currency, mapping.Currency)) ||
		(response.Check.Address != "" && !equalAddress(mapping.Currency, response.Check.Address, request.Address)) ||
		(response.Check.Transaction != nil && *response.Check.Transaction != request.TxHash) {
		return aml.Ticket{}, &ProtocolError{Kind: "invalid_submit_ack", SubmissionMayExist: true}
	}
	if err := validateTokenEcho(mapping, response.Check.TokenID); err != nil {
		return aml.Ticket{}, &ProtocolError{Kind: "submit_token_binding", SubmissionMayExist: true}
	}
	return aml.Ticket{Provider: ProviderName, ProviderAccountID: c.config.AccountID, CheckHash: response.Check.Hash,
		SubmitRequestID: requestID, RequestDigest: request.Digest(mapping.Version), MappingVersion: mapping.Version,
		Request: request, SubmittedAt: c.now().UTC()}, nil
}

func (c *Client) GetResult(ctx context.Context, ticket aml.Ticket, requestID string) (aml.CheckResult, error) {
	mapping, err := c.mapping(ticket.Request)
	if err != nil {
		return aml.CheckResult{}, err
	}
	if ticket.Provider != ProviderName || ticket.ProviderAccountID != c.config.AccountID || ticket.CheckHash == "" ||
		ticket.MappingVersion != mapping.Version || ticket.RequestDigest != ticket.Request.Digest(mapping.Version) {
		return aml.CheckResult{}, &ProtocolError{Kind: "ticket_binding"}
	}
	result, raw, err := call(c, ctx, "checkup.getresult", requestID, struct {
		Hash string `json:"hash"`
	}{Hash: ticket.CheckHash}, false)
	if err != nil {
		return aml.CheckResult{}, err
	}
	var response checkResponse
	if err := json.Unmarshal(result, &response); err != nil || response.Check.Hash != ticket.CheckHash {
		return aml.CheckResult{}, &ProtocolError{Kind: "result_binding"}
	}
	check := response.Check
	if slices.Contains(c.config.PendingStatuses, check.Status) {
		return aml.CheckResult{State: aml.CheckPending, ProviderStatus: check.Status}, nil
	}
	if slices.Contains(c.config.FailureStatuses, check.Status) {
		return aml.CheckResult{State: aml.CheckFailed, ProviderStatus: check.Status}, nil
	}
	if check.Status != "SUCCESS" {
		return aml.CheckResult{}, &ProtocolError{Kind: "unknown_status"}
	}
	if !strings.EqualFold(check.Currency, mapping.Currency) || !equalAddress(mapping.Currency, check.Address, ticket.Request.Address) ||
		(ticket.Request.Kind == aml.TransferSubject && (check.Transaction == nil || *check.Transaction != ticket.Request.TxHash)) ||
		(ticket.Request.Kind == aml.AddressSubject && check.Transaction != nil && *check.Transaction != "") {
		return aml.CheckResult{}, &ProtocolError{Kind: "subject_binding"}
	}
	if err := validateTokenEcho(mapping, check.TokenID); err != nil {
		return aml.CheckResult{}, err
	}
	report, err := normalizeReport(check, ticket, c.config.ContractVersion, raw, c.now().UTC())
	if err != nil {
		return aml.CheckResult{}, err
	}
	return aml.CheckResult{State: aml.CheckSucceeded, ProviderStatus: check.Status, Report: &report}, nil
}

func validateTokenEcho(mapping AssetMapping, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil // accepted request/ticket contract supplies the token scope
	}
	tokenID := string(raw)
	if strings.HasPrefix(tokenID, "\"") {
		if err := json.Unmarshal(raw, &tokenID); err != nil {
			return &ProtocolError{Kind: "token_binding"}
		}
	}
	if tokenID == mapping.ProviderTokenID || (mapping.Native && tokenID == "0") {
		return nil
	}
	return &ProtocolError{Kind: "token_binding"}
}

func equalAddress(currency, a, b string) bool {
	if currency == "ETH" {
		return strings.EqualFold(a, b)
	}
	return a == b // TRON Base58 identifiers are case-sensitive
}

type rpcRequest[P any] struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  P      `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
	Code    int             `json:"code"`
}

func call[P any](c *Client, ctx context.Context, method, requestID string, params P, submit bool) (json.RawMessage, []byte, error) {
	if requestID == "" || len(requestID) > 32 || strings.ContainsAny(requestID, "\x00\r\n\t ") {
		return nil, nil, &ProtocolError{Kind: "request_id"}
	}
	encoded, err := json.Marshal(rpcRequest[P]{JSONRPC: "2.0", ID: requestID, Method: method, Params: params})
	if err != nil {
		return nil, nil, &ProtocolError{Kind: "request_encoding"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, _endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, nil, &ProtocolError{Kind: "request_encoding"}
	}
	request.GetBody = nil
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.config.BearerToken)
	response, err := c.http.Do(request) // exactly one application-level attempt
	if err != nil {
		return nil, nil, &ProtocolError{Kind: "transport", SubmissionMayExist: submit}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, _maxBody+1))
	if err != nil || len(body) > _maxBody {
		return nil, nil, &ProtocolError{Kind: "response_body", HTTPStatus: response.StatusCode, SubmissionMayExist: submit}
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, &ProtocolError{Kind: "http", HTTPStatus: response.StatusCode, SubmissionMayExist: submit && response.StatusCode >= 500}
	}
	var envelope rpcResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, &ProtocolError{Kind: "invalid_json", SubmissionMayExist: submit}
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		code := envelope.Code
		var rpcError struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(envelope.Error, &rpcError) == nil {
			code = rpcError.Code
		}
		return nil, nil, &ProtocolError{Kind: "rpc", HTTPStatus: response.StatusCode, RPCCode: code}
	}
	if envelope.JSONRPC != "2.0" || envelope.ID != requestID || len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil, nil, &ProtocolError{Kind: "envelope", SubmissionMayExist: submit}
	}
	return envelope.Result, body, nil
}

var _ aml.Provider = (*Client)(nil)
