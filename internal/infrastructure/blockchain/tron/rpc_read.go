package tron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"metapus/internal/domain/crypto"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
	integrationhttp "metapus/internal/infrastructure/integrations"
)

const (
	opHead          = "head"
	opBlock         = "block"
	opReceipt       = "receipt"
	opBlockReceipts = "block_receipts"
)

func rpcErrorCategory(err error) string {
	var unavailable *integrations.PoolUnavailableError
	if errors.As(err, &unavailable) {
		switch unavailable.Reason {
		case integrations.Exhausted:
			return "pool_exhausted"
		case integrations.KeyMissing:
			return "pool_key_missing"
		}
		return "pool_unavailable"
	}
	switch {
	case errors.Is(err, crypto.ErrVerificationPending):
		return "verification_pending"
	case errors.Is(err, crypto.ErrVerificationMismatch):
		return "verification_mismatch"
	case errors.Is(err, errRPCHeadLagging):
		return "head_lagging"
	case errors.Is(err, crypto.ErrDecoderVersionConflict):
		return "decoder_version_conflict"
	case errors.Is(err, ErrUnsupportedTrace):
		return "unsupported_trace"
	case errors.Is(err, ErrInvalidTrace):
		return "invalid_trace"
	case err == nil:
		return ""
	case errors.Is(err, rpcpolicy.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, rpcpolicy.ErrAdmissionBusy):
		return "admission_busy"
	case errors.Is(err, rpcpolicy.ErrInvalidResponse):
		return "invalid_response"
	case errors.Is(err, rpcpolicy.ErrResponseTooLarge):
		return "response_limit"
	case errors.Is(err, rpcpolicy.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, integrationhttp.ErrAttemptBudget):
		return "account_attempt_budget"
	case errors.Is(err, integrationhttp.ErrAccessDenied):
		return "access_denied"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	var status *HTTPStatusError
	if errors.As(err, &status) {
		switch status.StatusCode {
		case http.StatusBadRequest:
			return "invalid_request"
		case http.StatusUnauthorized, http.StatusForbidden:
			return "authentication"
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			return "unsupported"
		}
		return "http_error"
	}
	return "transport_or_decode"
}

type RPCReadFailure struct {
	Provider, Operation, Category string
	cause                         error
}

func (e *RPCReadFailure) Error() string {
	return fmt.Sprintf("provider %s operation %s: %s", e.Provider, e.Operation, e.Category)
}
func (e *RPCReadFailure) Unwrap() error {
	if e.cause != nil {
		return e.cause
	}
	switch e.Category {
	case "timeout":
		return context.DeadlineExceeded
	case "rate_limited":
		return rpcpolicy.ErrRateLimited
	case "admission_busy":
		return rpcpolicy.ErrAdmissionBusy
	case "response_limit":
		return rpcpolicy.ErrResponseTooLarge
	case "invalid_response":
		return rpcpolicy.ErrInvalidResponse
	case "unsupported":
		return rpcpolicy.ErrUnsupported
	}
	return nil
}

func (e *rpcEndpoint) circuit(op string) *rpcpolicy.Circuit {
	c := e.operations[op]
	if c == nil {
		c = &rpcpolicy.Circuit{}
		e.operations[op] = c
	}
	return c
}
func (p *rpcGeneration) beginRead(e *rpcEndpoint, op string) bool {
	return p.beginReadContext(context.Background(), e, op)
}

func (p *rpcGeneration) beginReadContext(ctx context.Context, e *rpcEndpoint, op string) bool {
	// Lagging Solidity endpoints require a head probe before serving finality.
	e.breaker.mu.Lock()
	invalid := e.breaker.state == BreakerInvalid || (e.breaker.state == BreakerLagging && op != opHead && ctx.Value(recoveryEndpointKey{}) != e)
	e.breaker.mu.Unlock()
	if invalid {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if p.now().Before(e.unavailableUntil) {
		return false
	}
	return e.circuit(op).Begin(p.now())
}
func (p *rpcGeneration) finishRead(e *rpcEndpoint, op string, elapsed time.Duration, err error, parentErr error) {
	category := rpcErrorCategory(err)
	var retryAfter time.Time
	var throttle *rpcpolicy.ThrottleError
	if errors.As(err, &throttle) {
		retryAfter = throttle.Until
	}
	e.mu.Lock()
	c := e.circuit(op)
	c.Finish(p.now(), elapsed, category, parentErr != nil, p.threshold, p.openFor, retryAfter)
	if parentErr == nil && (category == "unsupported" || category == "authentication") {
		c.Unsupported = true
		c.LastError = category
	}
	e.mu.Unlock()
	if category == "rate_limited" && parentErr == nil {
		e.admission.Throttle(retryAfter)
	}
	var unavailable *integrations.PoolUnavailableError
	if parentErr == nil && errors.As(err, &unavailable) {
		// Recheck DB eligibility at most once per topology polling interval so
		// credential edits can recover a pool before its original quota reset.
		until := p.now().Add(10 * time.Second)
		if unavailable.RetryAt.After(p.now()) && unavailable.RetryAt.Before(until) {
			until = unavailable.RetryAt
		}
		for _, endpoints := range p.byRole {
			for _, candidate := range endpoints {
				if candidate != e && (e.poolScope == "" || candidate.poolScope != e.poolScope) {
					continue
				}
				candidate.mu.Lock()
				candidate.unavailableUntil, candidate.unavailableReason = until, category
				// Pool state is not a permanent method capability failure.
				if candidate == e {
					candidate.circuit(op).OpenUntil = until
				}
				candidate.mu.Unlock()
			}
		}
	}
}
func (p *rpcGeneration) readOrder(role chainconfig.Role, op string) []*rpcEndpoint {
	key := string(role) + ":" + op
	p.mu.Lock()
	sticky := p.preferred[key]
	if p.probeNext == nil {
		p.probeNext = make(map[string]uint64)
	}
	next := p.probeNext[key]
	p.probeNext[key]++
	p.mu.Unlock()
	now := p.now()
	var probes, healthy, unavailable []*rpcEndpoint
	for _, e := range p.byRole[role] {
		e.mu.Lock()
		c := e.operations[op]
		switch {
		case e.unavailableUntil.After(now):
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
	// One recovery attempt leaves capacity for healthy providers; rotate it so
	// several expired circuits cannot repeatedly hide a working reserve.
	if p.settings.MaxAttempts == 1 && len(healthy) > 0 && next%2 == 1 {
		return append(append(healthy, probes...), unavailable...)
	}
	result := append([]*rpcEndpoint{probes[0]}, healthy...)
	return append(append(result, probes[1:]...), unavailable...)
}

func endpointRead[T any](ctx context.Context, p *rpcGeneration, e *rpcEndpoint, op string, call func(context.Context, *Client) (T, error)) (T, error) {
	var zero T
	attempt, cancel := context.WithTimeout(ctx, p.settings.AttemptTimeout())
	defer cancel()
	span, started := beginRPCRequest(ctx, p.networkCode, e, op)
	leave, err := e.admission.Enter(attempt, op == opBlock || op == opBlockReceipts)
	if err != nil {
		err = errors.Join(rpcpolicy.ErrAdmissionBusy, ctx.Err())
	} else {
		defer leave()
	}
	var value T
	if err == nil {
		value, err = call(attempt, e.client)
	}
	p.finishRead(e, op, time.Since(started), err, ctx.Err())
	span.SetAttributes(rpcOperationAttribute(op))
	outcome := rpcErrorCategory(err)
	if outcome == "" {
		outcome = "success"
	}
	finishRPCRequest(ctx, span, started, p.networkCode, e, outcome, err, op)
	if err != nil {
		return zero, err
	}
	return value, nil
}

func rpcRead[T any](ctx context.Context, p *rpcGeneration, role chainconfig.Role, op string, call func(context.Context, *Client) (T, error)) (T, error) {
	var zero T
	readCtx, cancel := context.WithTimeout(ctx, p.settings.ReadBudget())
	defer cancel()
	var failures []error
	attempts := 0
	for _, e := range p.readOrder(role, op) {
		if readCtx.Err() != nil || attempts >= p.settings.MaxAttempts {
			break
		}
		if !p.beginRead(e, op) {
			recordRPCCircuitSkip(ctx, p.networkCode, e)
			continue
		}
		attempts++
		value, err := endpointRead(readCtx, p, e, op, call)
		if err == nil {
			p.mu.Lock()
			p.preferred[string(role)+":"+op] = e
			p.mu.Unlock()
			return value, nil
		}
		var unavailable *integrations.PoolUnavailableError
		if errors.As(err, &unavailable) {
			// An account pool with no eligible credentials is not another node
			// attempt. Preserve capacity to reach a later independent provider.
			attempts--
		}
		category := rpcErrorCategory(err)
		if category == "invalid_request" {
			return zero, fmt.Errorf("%s %s validation error: %w", role, e.providerID, rpcpolicy.ErrInvalidRequest)
		}
		failures = append(failures, &RPCReadFailure{Provider: e.providerID, Operation: op, Category: category, cause: err})
	}
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if readCtx.Err() != nil {
		failures = append(failures, readCtx.Err())
	}
	if len(failures) == 0 {
		return zero, ErrAllRPCProvidersUnavailable
	}
	return zero, fmt.Errorf("%w for network %s role %s: %w", ErrAllRPCProvidersUnavailable, p.networkCode, role, errors.Join(failures...))
}
