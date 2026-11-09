package tron

import (
	"context"
	"errors"
	"fmt"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

var ErrAllRPCProvidersUnavailable = errors.New("all RPC providers are unavailable")

type BreakerState string

const (
	BreakerClosed   BreakerState = "closed"
	BreakerOpen     BreakerState = "open"
	BreakerHalfOpen BreakerState = "half_open"
	BreakerDegraded BreakerState = "degraded"
	BreakerLagging  BreakerState = "lagging"
	BreakerInvalid  BreakerState = "invalid"
)

type EndpointStatus struct {
	EndpointID      id.ID                              `json:"endpointId,omitempty"`
	AppliedRevision string                             `json:"appliedRevision,omitempty"`
	Operations      []crypto.RuntimeRPCOperationStatus `json:"operations,omitempty"`
	ProviderID      string                             `json:"providerId"`
	Role            chainconfig.Role                   `json:"role"`
	State           BreakerState                       `json:"state"`
	Failures        int                                `json:"failures"`
	OpenUntil       time.Time                          `json:"openUntil"`
	LastError       string                             `json:"lastError,omitempty"`
}

type breaker struct {
	mu        sync.Mutex
	state     BreakerState
	failures  int
	openUntil time.Time
	lastError string
}

func (b *breaker) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == BreakerOpen && !now.Before(b.openUntil) {
		b.state = BreakerHalfOpen
		return true
	}
	return b.state != BreakerOpen && b.state != BreakerInvalid && b.state != BreakerLagging
}

func (b *breaker) lagging(best, actual int64) {
	b.mu.Lock()
	b.state = BreakerLagging
	b.lastError = fmt.Sprintf("head height lag: previous_or_corroborated=%d actual=%d", best, actual)
	b.mu.Unlock()
}

func (b *breaker) success() {
	b.mu.Lock()
	b.state = BreakerClosed
	b.failures = 0
	b.openUntil = time.Time{}
	b.lastError = ""
	b.mu.Unlock()
}

func (b *breaker) transientFailure(err error, threshold int, openFor time.Duration, now time.Time) {
	b.mu.Lock()
	b.failures++
	b.lastError = sanitizeRPCError(err)
	b.state = BreakerDegraded
	if b.failures >= threshold {
		b.state = BreakerOpen
		b.openUntil = now.Add(openFor)
	}
	b.mu.Unlock()
}

func (b *breaker) snapshot(provider string, role chainconfig.Role) EndpointStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return EndpointStatus{ProviderID: provider, Role: role, State: b.state, Failures: b.failures, OpenUntil: b.openUntil, LastError: b.lastError}
}

type rpcEndpoint struct {
	endpointID        id.ID
	providerID        string
	trustGroup        string
	role              chainconfig.Role
	client            *Client
	breaker           breaker // Broadcast uncertainty and Solidity lag only; reads use operation circuits.
	mu                sync.Mutex
	health            endpointHealth
	operations        map[string]*rpcpolicy.Circuit
	admission         *rpcpolicy.Admission
	poolScope         string
	unavailableUntil  time.Time
	unavailableReason string
}

type RPCPoolConfig struct {
	Reload           func(context.Context) (RPCPoolConfig, string, error)
	InitialVersion   string
	NetworkCode      string
	FailureThreshold int
	OpenDuration     time.Duration
	FullNode         []chainconfig.Endpoint
	Solidity         []chainconfig.Endpoint
	Settings         chainconfig.TRONSettings
}

// RPCPool owns role-specific endpoint failover. Each underlying client makes
// one attempt; retry across providers is performed here so breaker state is
// attributable to (network, provider, role).
type rpcGeneration struct {
	networkCode string
	threshold   int
	openFor     time.Duration
	byRole      map[chainconfig.Role][]*rpcEndpoint
	now         func() time.Time
	settings    chainconfig.TRONSettings
	mu          sync.Mutex
	preferred   map[string]*rpcEndpoint
	probeNext   map[string]uint64
	closeOnce   sync.Once
}

func newRPCGeneration(cfg RPCPoolConfig) (*rpcGeneration, error) {
	if cfg.NetworkCode == "" {
		return nil, errors.New("RPC pool network code is required")
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 30 * time.Second
	}
	cfg.Settings = cfg.Settings.Defaults()
	if err := cfg.Settings.Validate(); err != nil {
		return nil, err
	}
	p := &rpcGeneration{
		settings: cfg.Settings, preferred: make(map[string]*rpcEndpoint),
		networkCode: cfg.NetworkCode,
		threshold:   cfg.FailureThreshold,
		openFor:     cfg.OpenDuration,
		byRole:      make(map[chainconfig.Role][]*rpcEndpoint, 3),
		now:         time.Now,
	}
	for role, configs := range map[chainconfig.Role][]chainconfig.Endpoint{
		chainconfig.RoleFullNode: cfg.FullNode,
		chainconfig.RoleSolidity: cfg.Solidity,
	} {
		if len(configs) == 0 {
			p.Close()
			return nil, fmt.Errorf("RPC pool %s has no %s endpoints", cfg.NetworkCode, role)
		}
		for _, endpoint := range configs {
			admission, err := rpcpolicy.AcquireScoped(endpoint.AdmissionScope, endpoint.URL, rpcpolicy.Capacity{Concurrency: cfg.Settings.ProviderConcurrency, BulkConcurrency: cfg.Settings.BackfillConcurrency, RequestsPerSecond: cfg.Settings.RequestsPerSecond, MinRequestIntervalMS: endpoint.MinRequestIntervalMS})
			if err != nil {
				p.Close()
				return nil, err
			}
			p.byRole[role] = append(p.byRole[role], &rpcEndpoint{
				endpointID: endpoint.EndpointID,
				providerID: endpoint.ProviderID,
				trustGroup: endpoint.TrustIdentity(),
				role:       role,
				client:     NewClient(ClientConfig{WrapTransport: endpoint.WrapTransport, BaseURL: endpoint.URL, APIKey: endpoint.APIKey, MaxRetries: 1, MaxResponseBytes: cfg.Settings.MaxResponseBytes, DisableKeepAlives: endpoint.DisableKeepAlives}),
				breaker:    breaker{state: BreakerClosed},
				admission:  admission, operations: make(map[string]*rpcpolicy.Circuit),
				poolScope: endpoint.AdmissionScope,
			})
		}
	}
	return p, nil
}

func (p *rpcGeneration) Status() []EndpointStatus {
	var result []EndpointStatus
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		for _, endpoint := range p.byRole[role] {
			status := endpoint.breaker.snapshot(endpoint.providerID, role)
			status.EndpointID = endpoint.endpointID
			status.LastError = rpcpolicy.SafeReason(status.LastError)
			if status.State == BreakerLagging {
				status.LastError = "head_lagging"
			}
			endpoint.mu.Lock()
			aggregate := crypto.RuntimeEndpointStatus{State: string(status.State), Failures: status.Failures, LastError: status.LastError}
			operations := make([]string, 0, len(endpoint.operations))
			for op := range endpoint.operations {
				operations = append(operations, op)
			}
			sort.Strings(operations)
			for _, op := range operations {
				c := endpoint.operations[op]
				rpcpolicy.AddDiagnostic(&aggregate, c.Diagnostic(op))
				if c.OpenUntil.After(status.OpenUntil) {
					status.OpenUntil = c.OpenUntil
				}
			}
			status.State, status.Failures, status.LastError, status.Operations = BreakerState(aggregate.State), aggregate.Failures, aggregate.LastError, aggregate.Operations
			if endpoint.unavailableUntil.After(p.now()) {
				status.State, status.OpenUntil, status.LastError = BreakerOpen, endpoint.unavailableUntil, rpcpolicy.SafeReason(endpoint.unavailableReason)
			}
			endpoint.mu.Unlock()
			result = append(result, status)
		}
	}
	return result
}

// Probe exercises real role operations. Successful heads only repair the
// head circuit; they never close a failed events/account pagination circuit.
func (p *rpcGeneration) Probe(ctx context.Context) {
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		heads := make(map[*rpcEndpoint]*BlockInfo)
		for _, e := range p.byRole[role] {
			if ctx.Err() != nil {
				return
			}
			if !p.beginRead(e, opHead) {
				continue
			}
			block, err := endpointRead(ctx, p, e, opHead, func(attempt context.Context, c *Client) (*BlockInfo, error) {
				var block *BlockInfo
				var err error
				if role == chainconfig.RoleSolidity {
					block, err = c.GetSolidifiedCurrentBlockInfo(attempt)
				} else {
					block, err = c.GetCurrentBlockInfo(attempt)
				}
				if err == nil {
					err = p.observeHead(e, block)
				}
				return block, err
			})
			if err == nil {
				heads[e] = block
			} else {
				e.mu.Lock()
				e.health.recoveryAt = time.Time{}
				e.health.recoveryHeight = 0
				e.mu.Unlock()
			}
		}
		p.quarantineRelativeLag(heads)
		for e, head := range heads {
			if endpointQuarantined(e) {
				p.recoverEndpoint(ctx, e, head)
			} else {
				e.breaker.success()
			}
		}
	}
}

func isTransientRPCError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, rpcpolicy.ErrRateLimited) || errors.Is(err, rpcpolicy.ErrAdmissionBusy) || errors.Is(err, rpcpolicy.ErrResponseTooLarge) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if statusErr, ok := errors.AsType[*HTTPStatusError](err); ok {
		return statusErr.Transient()
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func sanitizeRPCError(err error) string {
	if err == nil {
		return ""
	}
	return rpcErrorCategory(err)
}

func (p *rpcGeneration) CreateTransferTransaction(ctx context.Context, from, to string, amount int64) (*UnsignedTransaction, error) {
	return rpcRead(ctx, p, chainconfig.RoleFullNode, "CreateTransferTransaction", func(attempt context.Context, c *Client) (*UnsignedTransaction, error) {
		return c.CreateTransferTransaction(attempt, from, to, amount)
	})
}

func (p *rpcGeneration) CreateAccountTransaction(ctx context.Context, owner, account string) (*UnsignedTransaction, error) {
	return rpcRead(ctx, p, chainconfig.RoleFullNode, "CreateAccountTransaction", func(attempt context.Context, c *Client) (*UnsignedTransaction, error) {
		return c.CreateAccountTransaction(attempt, owner, account)
	})
}

func (p *rpcGeneration) TriggerSmartContract(ctx context.Context, params TriggerContractParams) (*UnsignedTransaction, error) {
	return rpcRead(ctx, p, chainconfig.RoleFullNode, "TriggerSmartContract", func(attempt context.Context, c *Client) (*UnsignedTransaction, error) {
		return c.TriggerSmartContract(attempt, params)
	})
}

func (p *rpcGeneration) EstimateEnergy(ctx context.Context, params TriggerContractParams) (EnergyEstimate, error) {
	return rpcRead(ctx, p, chainconfig.RoleFullNode, "EstimateEnergy", func(attempt context.Context, c *Client) (EnergyEstimate, error) {
		return c.EstimateEnergy(attempt, params)
	})
}

func (p *rpcGeneration) GetAccountResource(ctx context.Context, address string) (*AccountResourceResponse, error) {
	return rpcRead(ctx, p, chainconfig.RoleFullNode, "GetAccountResource", func(attempt context.Context, c *Client) (*AccountResourceResponse, error) {
		return c.GetAccountResource(attempt, address)
	})
}

func (p *rpcGeneration) GetTRXBalance(ctx context.Context, address string) (types.CryptoAmount, error) {
	return rpcRead(ctx, p, chainconfig.RoleSolidity, "GetTRXBalance", func(attempt context.Context, c *Client) (types.CryptoAmount, error) {
		return c.GetSolidifiedTRXBalance(attempt, address)
	})
}

func (p *rpcGeneration) GetTRC20Balance(ctx context.Context, address, contract string) (types.CryptoAmount, error) {
	return rpcRead(ctx, p, chainconfig.RoleSolidity, "GetTRC20Balance", func(attempt context.Context, c *Client) (types.CryptoAmount, error) {
		return c.GetSolidifiedTRC20Balance(attempt, address, contract)
	})
}

func (p *rpcGeneration) AccountExists(ctx context.Context, address string) (bool, error) {
	return rpcRead(ctx, p, chainconfig.RoleSolidity, "AccountExists", func(attempt context.Context, c *Client) (bool, error) {
		resp, err := c.GetSolidifiedAccount(attempt, address)
		return resp != nil && (strings.TrimSpace(resp.Address) != "" || resp.CreateTime > 0 || resp.Balance > 0), err
	})
}

func (p *rpcGeneration) GetTransactionInfo(ctx context.Context, txHash string) (*TransactionInfo, error) {
	return rpcRead(ctx, p, chainconfig.RoleSolidity, "receipt", func(attempt context.Context, c *Client) (*TransactionInfo, error) {
		return c.GetSolidifiedTransactionInfo(attempt, txHash)
	})
}

func (p *rpcGeneration) GetCurrentBlock(ctx context.Context) (int64, error) {
	return rpcRead(ctx, p, chainconfig.RoleSolidity, "head", func(attempt context.Context, c *Client) (int64, error) {
		block, err := c.GetSolidifiedCurrentBlockInfo(attempt)
		if err != nil {
			return 0, err
		}
		return block.BlockHeader.RawData.Number, nil
	})
}

func (p *rpcGeneration) GetCurrentBlockInfo(ctx context.Context) (*BlockInfo, error) {
	return rpcRead(ctx, p, chainconfig.RoleSolidity, "head", func(attempt context.Context, c *Client) (*BlockInfo, error) {
		block, err := c.GetSolidifiedCurrentBlockInfo(attempt)
		if err == nil && (block == nil || block.BlockID == "" || block.BlockHeader.RawData.Number <= 0) {
			return nil, rpcpolicy.ErrInvalidResponse
		}
		return block, err
	})
}

func (p *rpcGeneration) GetSolidifiedBlockByNumber(ctx context.Context, number int64) (*BlockInfo, error) {
	return rpcRead(ctx, p, chainconfig.RoleSolidity, "block", func(attempt context.Context, c *Client) (*BlockInfo, error) {
		block, err := c.GetSolidifiedBlockByNumber(attempt, number)
		if err == nil && (block == nil || block.BlockID == "" || block.BlockHeader.RawData.Number != number) {
			return nil, rpcpolicy.ErrInvalidResponse
		}
		return block, err
	})
}

// BroadcastSignedEnvelope preserves the exact signed payload across every
// provider. A transient POST result is ambiguous: lookup happens before the
// byte-identical transaction is submitted to the next healthy provider.
func (p *rpcGeneration) BroadcastSignedEnvelope(ctx context.Context, envelope crypto.SignedTxEnvelope) (crypto.BroadcastOutcome, error) {
	signed, err := decodeTRONSignedEnvelope(envelope)
	if err != nil {
		return crypto.BroadcastOutcome{Status: crypto.BroadcastRejected, Reason: err.Error()}, nil
	}
	var ambiguous bool
	for _, endpoint := range p.byRole[chainconfig.RoleFullNode] {
		if !endpoint.breaker.allow(p.now()) {
			recordRPCCircuitSkip(ctx, p.networkCode, endpoint)
			continue
		}
		span, started := beginRPCRequest(ctx, p.networkCode, endpoint)
		result, err := endpoint.client.BroadcastTransaction(ctx, signed)
		if err == nil {
			endpoint.breaker.success()
			if result.Result {
				finishRPCRequest(ctx, span, started, p.networkCode, endpoint, "accepted", nil)
				return crypto.BroadcastOutcome{Status: crypto.BroadcastAccepted, TxHash: envelope.TxHash, ProviderID: endpoint.providerID}, nil
			}
			if strings.Contains(strings.ToUpper(result.Code), "DUP") || strings.Contains(strings.ToUpper(result.Message), "DUP") {
				finishRPCRequest(ctx, span, started, p.networkCode, endpoint, "already_known", nil)
				return crypto.BroadcastOutcome{Status: crypto.BroadcastAlreadyKnown, TxHash: envelope.TxHash, ProviderID: endpoint.providerID}, nil
			}
			finishRPCRequest(ctx, span, started, p.networkCode, endpoint, "rejected", nil)
			return crypto.BroadcastOutcome{Status: crypto.BroadcastRejected, TxHash: envelope.TxHash, ProviderID: endpoint.providerID, Reason: result.Code}, nil
		}
		if !isTransientRPCError(err) {
			finishRPCRequest(ctx, span, started, p.networkCode, endpoint, "validation_error", err)
			return crypto.BroadcastOutcome{Status: crypto.BroadcastRejected, TxHash: envelope.TxHash, ProviderID: endpoint.providerID, Reason: sanitizeRPCError(err)}, nil
		}
		ambiguous = true
		endpoint.breaker.transientFailure(err, p.threshold, p.openFor, p.now())
		finishRPCRequest(ctx, span, started, p.networkCode, endpoint, "ambiguous", err)
		if p.transactionVisible(ctx, envelope.TxHash) {
			return crypto.BroadcastOutcome{Status: crypto.BroadcastAlreadyKnown, TxHash: envelope.TxHash, ProviderID: endpoint.providerID}, nil
		}
	}
	if ambiguous {
		recordAmbiguousRPCBroadcast(ctx, p.networkCode)
		return crypto.BroadcastOutcome{Status: crypto.BroadcastAmbiguous, TxHash: envelope.TxHash, Reason: "broadcast result remains ambiguous after provider lookup"}, nil
	}
	return crypto.BroadcastOutcome{}, fmt.Errorf("%w for network %s role %s", ErrAllRPCProvidersUnavailable, p.networkCode, chainconfig.RoleFullNode)
}

func decodeTRONSignedEnvelope(envelope crypto.SignedTxEnvelope) (SignedTransaction, error) {
	if envelope.Family != crypto.ChainFamilyTRON || envelope.SchemaVersion != tronEnvelopeSchemaVersion ||
		strings.TrimSpace(envelope.TxHash) == "" || strings.TrimSpace(envelope.UnsignedFingerprint) == "" {
		return SignedTransaction{}, errors.New("invalid TRON signed envelope")
	}
	var signed SignedTransaction
	if err := json.Unmarshal(envelope.Payload, &signed); err != nil {
		return SignedTransaction{}, errors.New("invalid signed payload")
	}
	if signed.TxID != envelope.TxHash {
		return SignedTransaction{}, errors.New("signed payload tx hash mismatch")
	}
	return signed, nil
}

func (p *rpcGeneration) transactionVisible(ctx context.Context, txHash string) bool {
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		for _, endpoint := range p.byRole[role] {
			if !endpoint.breaker.allow(p.now()) {
				recordRPCCircuitSkip(ctx, p.networkCode, endpoint)
				continue
			}
			span, started := beginRPCRequest(ctx, p.networkCode, endpoint)
			var info *TransactionInfo
			var err error
			if role == chainconfig.RoleSolidity {
				info, err = endpoint.client.GetSolidifiedTransactionInfo(ctx, txHash)
			} else {
				info, err = endpoint.client.GetTransactionInfo(ctx, txHash)
			}
			if err == nil {
				endpoint.breaker.success()
				finishRPCRequest(ctx, span, started, p.networkCode, endpoint, "success", nil)
				if info.BlockNumber > 0 || info.BlockTimestamp > 0 || info.Fee > 0 || len(info.Log) > 0 {
					return true
				}
				continue
			}
			if isTransientRPCError(err) {
				endpoint.breaker.transientFailure(err, p.threshold, p.openFor, p.now())
				finishRPCRequest(ctx, span, started, p.networkCode, endpoint, "transient_error", err)
			} else {
				finishRPCRequest(ctx, span, started, p.networkCode, endpoint, "validation_error", err)
			}
		}
	}
	return false
}

// Compile-time guards for watcher use and error classification imports.

// Close releases all endpoint capacity references and owned idle connections.
func (p *rpcGeneration) Close() {
	p.closeOnce.Do(func() {
		for _, endpoints := range p.byRole {
			for _, e := range endpoints {
				e.client.httpClient.CloseIdleConnections()
				rpcpolicy.Release(e.admission)
			}
		}
	})
}
