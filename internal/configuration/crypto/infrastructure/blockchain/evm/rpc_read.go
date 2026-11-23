package evm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/rpcpolicy"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/providerpolicy"
)

type rpcOperation string

const (
	opHeader  rpcOperation = "header"
	opBlock   rpcOperation = "full_block"
	opLogs    rpcOperation = "logs"
	opReceipt rpcOperation = "receipt"
	opState   rpcOperation = "state"
)

var (
	ErrRPCResponseTooLarge = providerpolicy.ErrResponseTooLarge
	ErrRPCUnsupported      = providerpolicy.ErrUnsupported
	ErrRPCAdmissionBusy    = providerpolicy.ErrAdmissionBusy
	ErrRPCRateLimited      = providerpolicy.ErrRateLimited
	ErrRPCInvalidRequest   = providerpolicy.ErrInvalidRequest
	ErrRPCHeadBehind       = errors.New("EVM RPC provider head behind requested range")
	ErrRPCStaleHead        = errors.New("EVM RPC provider returned stale head")
	ErrRPCInvalidHeader    = errors.New("EVM RPC header does not match its advertised hash or height")
)

type operationCircuit = rpcpolicy.Circuit
type providerAdmission = providerpolicy.Admission

func acquireProviderAdmission(endpoint chainconfig.Endpoint, s chainconfig.EVMSettings) (*providerAdmission, error) {
	return providerpolicy.AcquireScoped(endpoint.AdmissionScope, endpoint.URL, providerpolicy.Capacity{Concurrency: s.ProviderConcurrency, BulkConcurrency: s.BackfillConcurrency, RequestsPerSecond: s.RequestsPerSecond, MinRequestIntervalMS: endpoint.MinRequestIntervalMS})
}
func releaseProviderAdmission(a *providerAdmission) { providerpolicy.Release(a) }

// The response cap includes chunked bodies. A length check alone would miss
// providers that begin a partial JSON response and then stall indefinitely.
type boundedRPCTransport struct {
	base              http.RoundTripper
	limit             int64
	network, provider string
}

func (t boundedRPCTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	op, _ := req.Context().Value(rpcOperationContextKey{}).(rpcOperation)
	if err = providerpolicy.LimitResponse(resp, t.limit, func(bytes int64) {
		evmOperationalMetrics.rpcResponseBytes.Add(req.Context(), bytes, rpcMetricAttrs(t.network, t.provider, string(op)))
	}); err != nil {
		return nil, err
	}
	return resp, nil
}

type rpcOperationContextKey struct{}

// poolUnavailableCategory reports why no account of an endpoint pool could be
// used. No request reached the provider, so this is not a transport failure.
func poolUnavailableCategory(err error) string {
	var unavailable *integrations.PoolUnavailableError
	errors.As(err, &unavailable)
	switch unavailable.Reason {
	case integrations.Exhausted:
		return "pool_exhausted"
	case integrations.KeyMissing:
		return "pool_key_missing"
	}
	return "pool_unavailable"
}

func rpcErrorCategory(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrRPCRateLimited):
		return "rate_limited"
	case errors.Is(err, ErrRPCAdmissionBusy):
		return "admission_busy"
	case errors.Is(err, ErrRPCUnsupported):
		return "unsupported"
	case errors.Is(err, providerpolicy.ErrCredentialsExpired):
		return "credentials_expired"
	case errors.As(err, new(*integrations.PoolUnavailableError)):
		return poolUnavailableCategory(err)
	case errors.Is(err, providerpolicy.ErrAccessDenied):
		return "access_denied"
	case errors.Is(err, providerpolicy.ErrQueryLimit):
		return "query_limit"
	case errors.Is(err, providerpolicy.ErrArchiveUnavailable):
		return "archive_unavailable"
	case errors.Is(err, ErrRPCHeadBehind):
		return "head_behind"
	case errors.Is(err, ErrRPCStaleHead):
		return "stale_head"
	case errors.Is(err, ErrRPCInvalidHeader):
		return "invalid_header"
	case errors.Is(err, ErrRPCInvalidRequest):
		return "invalid_request"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ethereum.NotFound):
		return "not_found"
	case errors.Is(err, ErrRPCResponseTooLarge):
		return "response_limit"
	}
	var httpErr rpc.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == http.StatusTooManyRequests {
			return "rate_limited"
		}
		return "http_error"
	}
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		// Chainstack's plan gate applies to historical requests, not to the
		// endpoint's ability to serve the same method for recent blocks.
		if rpcErr.ErrorCode() == -32002 && strings.Contains(strings.ToLower(rpcErr.Error()), "archive, debug and trace requests are not available on your current plan") {
			return "archive_unavailable"
		}
		switch rpcErr.ErrorCode() {
		case -32601:
			return "unsupported"
		case -32600, -32602:
			return "invalid_request"
		}
		return "rpc_error"
	}
	return "transport_or_decode"
}

func (p *rpcGeneration) beginRead(endpoint *rpcEndpoint, op rpcOperation, now time.Time) bool {
	if p.hasWrongChainEndpoint() {
		return false
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if !endpoint.verified || endpoint.state == rpcStateWrongChain {
		return false
	}
	c := endpoint.operations[op]
	if c == nil {
		c = &operationCircuit{}
		endpoint.operations[op] = c
	}
	return c.Begin(now)
}
func (p *rpcGeneration) finishRead(endpoint *rpcEndpoint, op rpcOperation, elapsed time.Duration, err error, parentErr error) {
	category := rpcErrorCategory(err)
	endpoint.mu.Lock()
	c := endpoint.operations[op]
	var retryAfter time.Time
	var throttled *providerpolicy.ThrottleError
	if errors.As(err, &throttled) {
		retryAfter = throttled.Until
	}
	c.Finish(time.Now(), elapsed, category, parentErr != nil, p.failureThreshold, p.openDuration, retryAfter)
	until := c.OpenUntil
	endpoint.mu.Unlock()
	if category == "rate_limited" {
		endpoint.admission.Throttle(until)
	}
}
func (p *rpcGeneration) readOrder(op rpcOperation) []*rpcEndpoint {
	p.readMu.Lock()
	sticky := p.preferred[op]
	if p.probeNext == nil {
		p.probeNext = make(map[rpcOperation]uint64)
	}
	next := p.probeNext[op]
	p.probeNext[op]++
	p.readMu.Unlock()
	// Reserve at most one leading recovery attempt, leaving the other attempt
	// for healthy/untried endpoints. Rotate probes so repeated failures cannot
	// hide a third provider, even when every circuit expires during backoff.
	now := time.Now()
	var probes, healthy, unavailable []*rpcEndpoint
	for _, e := range p.endpoints {
		e.mu.Lock()
		c := e.operations[op]
		switch {
		case !e.verified || e.state == rpcStateWrongChain:
			unavailable = append(unavailable, e)
		case c != nil && c.Recoverable(now):
			probes = append(probes, e)
		case c != nil && (c.Unsupported || c.Probing || now.Before(c.OpenUntil)):
			unavailable = append(unavailable, e)
		default:
			if e == sticky {
				healthy = append([]*rpcEndpoint{e}, healthy...)
			} else {
				healthy = append(healthy, e)
			}
		}
		e.mu.Unlock()
	}
	if len(probes) == 0 {
		return append(healthy, unavailable...)
	}
	index := int(next % uint64(len(probes)))
	probes = append(probes[index:], probes[:index]...)
	if p.settings.MaxAttempts == 1 && len(healthy) > 0 && next%2 == 1 {
		return append(append(healthy, probes...), unavailable...)
	}
	result := append([]*rpcEndpoint{probes[0]}, healthy...)
	result = append(result, probes[1:]...)
	return append(result, unavailable...)
}

func endpointRead[T any](ctx context.Context, p *rpcGeneration, endpoint *rpcEndpoint, op rpcOperation, call func(context.Context, *ethclient.Client) (T, error)) (T, error) {
	var zero T
	timeout := p.settings.AttemptTimeout()
	if op == opLogs {
		timeout = time.Duration(p.settings.LogsTimeoutMS) * time.Millisecond
	}
	bulk := op == opBlock || op == opLogs
	attempt, cancel := context.WithTimeout(providerpolicy.WithBulk(context.WithValue(ctx, rpcOperationContextKey{}, op), bulk), timeout)
	defer cancel()
	started := time.Now()
	leave, err := endpoint.admission.Enter(attempt, bulk)
	if err != nil {
		err = errors.Join(ErrRPCAdmissionBusy, ctx.Err())
		p.finishRead(endpoint, op, time.Since(started), err, ctx.Err())
		return zero, err
	}
	attrs := rpcMetricAttrs(p.networkCode, endpoint.providerID, string(op))
	evmOperationalMetrics.rpcAdmission.Record(ctx, time.Since(started).Seconds(), attrs)
	evmOperationalMetrics.rpcInFlight.Add(ctx, 1, attrs)
	defer func() { leave(); evmOperationalMetrics.rpcInFlight.Add(ctx, -1, attrs) }()
	value, err := call(attempt, endpoint.client)
	p.finishRead(endpoint, op, time.Since(started), err, ctx.Err())
	endpoint.mu.Lock()
	c := endpoint.operations[op]
	open := int64(0)
	if c.Unsupported || !c.OpenUntil.IsZero() {
		open = 1
	}
	endpoint.mu.Unlock()
	evmOperationalMetrics.rpcCircuit.Record(ctx, open, attrs)
	recordRPCRead(ctx, p.networkCode, endpoint.providerID, string(op), time.Since(started), rpcErrorCategory(err))
	return value, err
}
func rpcRead[T any](ctx context.Context, p *rpcGeneration, op rpcOperation, method string, call func(context.Context, *ethclient.Client) (T, error)) (T, error) {
	return rpcReadEndpoint(ctx, p, op, method, func(attempt context.Context, endpoint *rpcEndpoint) (T, error) {
		return call(attempt, endpoint.client)
	})
}

func rpcReadEndpoint[T any](ctx context.Context, p *rpcGeneration, op rpcOperation, method string, call func(context.Context, *rpcEndpoint) (T, error)) (T, error) {
	var zero T
	budget := p.settings.ReadBudget()
	if op == opLogs {
		budget = max(budget, 2*time.Duration(p.settings.LogsTimeoutMS)*time.Millisecond)
	}
	readCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var joined []error
	notFound := false
	attempts := 0
	for _, endpoint := range p.readOrder(op) {
		if readCtx.Err() != nil {
			joined = append(joined, readCtx.Err())
			break
		}
		if attempts >= p.settings.MaxAttempts {
			break
		}
		endpoint.mu.Lock()
		unsupported := endpoint.unsupportedMethods[string(op)+":"+method]
		endpoint.mu.Unlock()
		if unsupported {
			joined = append(joined, &RPCReadFailure{Provider: endpoint.providerID, Operation: string(op), Category: "unsupported"})
			continue
		}
		if !p.beginRead(endpoint, op, time.Now()) {
			continue
		}
		attempts++
		value, err := endpointRead(readCtx, p, endpoint, op, func(attempt context.Context, _ *ethclient.Client) (T, error) {
			return call(attempt, endpoint)
		})
		if err == nil {
			if attempts > 1 {
				evmOperationalMetrics.rpcFailover.Add(ctx, 1, rpcMetricAttrs(p.networkCode, endpoint.providerID, string(op)))
			}
			p.readMu.Lock()
			p.preferred[op] = endpoint
			p.readMu.Unlock()
			return value, nil
		}
		if rpcErrorCategory(err) == "unsupported" {
			endpoint.mu.Lock()
			endpoint.unsupportedMethods[string(op)+":"+method] = true
			endpoint.mu.Unlock()
		}
		if errors.Is(err, ethereum.NotFound) {
			notFound = true
			continue
		}
		switch rpcErrorCategory(err) {
		case "invalid_request":
			return zero, fmt.Errorf("%w: provider %s", ErrRPCInvalidRequest, endpoint.providerID)
		}
		// Provider IDs are public configuration identifiers; transport errors may
		// contain credentials in URLs and never leave the adapter as status text.
		joined = append(joined, &RPCReadFailure{Provider: endpoint.providerID, Operation: string(op), Category: rpcErrorCategory(err)})
	}
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if notFound && len(joined) == 0 {
		return zero, ethereum.NotFound
	}
	if len(joined) == 0 {
		return zero, ErrAllRPCProvidersUnavailable
	}
	return zero, fmt.Errorf("%w: %w", ErrAllRPCProvidersUnavailable, errors.Join(joined...))
}

// RPCReadFailure exposes typed retry semantics without exposing request URLs.
type RPCReadFailure struct{ Provider, Operation, Category string }

func (e *RPCReadFailure) Error() string {
	return fmt.Sprintf("provider %s operation %s: %s", e.Provider, e.Operation, e.Category)
}
func (e *RPCReadFailure) Unwrap() error {
	switch e.Category {
	case "query_limit":
		return providerpolicy.ErrQueryLimit
	case "credentials_expired":
		return providerpolicy.ErrCredentialsExpired
	case "access_denied":
		return providerpolicy.ErrAccessDenied
	case "timeout":
		return context.DeadlineExceeded
	case "unsupported":
		return ErrRPCUnsupported
	case "response_limit":
		return ErrRPCResponseTooLarge
	case "rate_limited":
		return ErrRPCRateLimited
	case "admission_busy":
		return ErrRPCAdmissionBusy
	case "not_found":
		return ethereum.NotFound
	case "head_behind":
		return ErrRPCHeadBehind
	}
	return nil
}

func (t boundedRPCTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
