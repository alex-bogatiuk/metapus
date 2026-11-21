package tron

import (
	"context"
	"errors"
	"fmt"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
)

// endpointFailure preserves identity for routing while keeping upstream bodies
// and authenticated URLs out of errors and logs.
type endpointFailure struct {
	endpoint *rpcEndpoint
	cause    error
}

func (e *endpointFailure) Error() string {
	return fmt.Sprintf("provider %s: %s", e.endpoint.providerID, rpcErrorCategory(e.cause))
}
func (e *endpointFailure) Unwrap() error { return e.cause }

func excludeFailedGroups(err error, excluded map[string]bool) bool {
	if err == nil || errors.Is(err, crypto.ErrVerificationMismatch) || errors.Is(err, crypto.ErrVerificationPending) || semanticReadFailure(err) {
		return false
	}
	if failure, ok := err.(*endpointFailure); ok {
		if errors.Is(failure.cause, context.Canceled) || excluded[failure.endpoint.trustGroup] {
			return false
		}
		excluded[failure.endpoint.trustGroup] = true
		return true
	}
	changed := false
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if excludeFailedGroups(cause, excluded) {
				changed = true
			}
		}
	} else if cause := errors.Unwrap(err); cause != nil {
		changed = excludeFailedGroups(cause, excluded)
	}
	return changed
}

// Each retry pins a new independent pair and repeats the whole callback. A data
// mismatch never triggers provider shopping. The read budget bounds all pairs.
func withIndependentPair[T any](ctx context.Context, p *rpcGeneration, role chainconfig.Role, excluded map[string]bool, read func(context.Context, []*rpcEndpoint) (T, error)) (T, error) {
	var zero T
	readCtx, cancel := context.WithTimeout(ctx, p.settings.ReadBudget())
	defer cancel()
	if excluded == nil {
		excluded = make(map[string]bool)
	}
	var last error
	for range len(p.byRole[role]) + 1 {
		if readCtx.Err() != nil {
			return zero, errors.Join(crypto.ErrChainQuorumUnavailable, readCtx.Err(), last)
		}
		pair, err := p.independentPairExcept(role, excluded)
		if err != nil {
			return zero, errors.Join(err, last)
		}
		value, err := read(readCtx, pair)
		if err == nil {
			return value, nil
		}
		last = err
		if !excludeFailedGroups(err, excluded) {
			return zero, err
		}
	}
	return zero, errors.Join(crypto.ErrChainQuorumUnavailable, last)
}
