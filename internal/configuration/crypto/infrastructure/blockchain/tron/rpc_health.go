package tron

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/pkg/sdk/integrations/providerpolicy"
)

var errRPCHeadLagging = errors.New("RPC head is stale or regressed")

const (
	headRecoveryInterval       = 5 * time.Second
	maxHeadSkew          int64 = 20
)

type endpointHealth struct {
	height         int64
	recoveryHeight int64
	recoveryAt     time.Time
}

// Recovery reads retain normal operation circuits and admission limits. Only
// the lag quarantine is bypassed, for this endpoint and this private context.
type recoveryEndpointKey struct{}

// A large skew with apparently fresh timestamps needs corroboration. Confirm
// the higher range with another independent source before excluding the lower
// one. A single high outlier must not quarantine the healthy majority.
func (p *rpcGeneration) checkPairLag(ctx context.Context, pair []*rpcEndpoint, heads []*BlockInfo) error {
	low, high := 0, 1
	if heads[low].BlockHeader.RawData.Number > heads[high].BlockHeader.RawData.Number {
		low, high = high, low
	}
	lower := heads[low].BlockHeader.RawData.Number
	if heads[high].BlockHeader.RawData.Number-lower <= maxHeadSkew {
		return nil
	}
	for _, candidate := range p.readOrder(pair[0].role, opHead) {
		if endpointQuarantined(candidate) || candidate.trustGroup == "" || candidate.trustGroup == pair[0].trustGroup || candidate.trustGroup == pair[1].trustGroup || candidate.providerID == pair[0].providerID || candidate.providerID == pair[1].providerID || candidate.client.baseURL == pair[0].client.baseURL || candidate.client.baseURL == pair[1].client.baseURL {
			continue
		}
		head, err := p.endpointHead(ctx, candidate)
		if err != nil || head.BlockHeader.RawData.Number-lower <= maxHeadSkew {
			continue
		}
		height := min(head.BlockHeader.RawData.Number, heads[high].BlockHeader.RawData.Number)
		reference := heads[high]
		if reference.BlockHeader.RawData.Number != height {
			reference, err = p.endpointBlock(ctx, pair[high], height)
			if err != nil {
				return &endpointFailure{endpoint: pair[high], cause: err}
			}
		}
		if head.BlockHeader.RawData.Number != height {
			head, err = p.endpointBlock(ctx, candidate, height)
			if err != nil {
				continue
			}
		}
		if !strings.EqualFold(reference.BlockID, head.BlockID) {
			return crypto.ErrVerificationMismatch
		}
		pair[low].breaker.lagging(height, lower)
		return &endpointFailure{endpoint: pair[low], cause: errRPCHeadLagging}
	}
	return nil
}

func (p *rpcGeneration) observeHead(e *rpcEndpoint, block *BlockInfo) error {
	if block == nil || block.BlockHeader.RawData.Number <= 0 || !validHash(block.BlockID) || block.BlockHeader.RawData.Timestamp <= 0 {
		return providerpolicy.ErrInvalidResponse
	}
	height := block.BlockHeader.RawData.Number
	age := p.now().Sub(time.UnixMilli(block.BlockHeader.RawData.Timestamp))
	maxAge := 90 * time.Second
	if e.role == chainconfig.RoleFullNode {
		maxAge = 30 * time.Second
	}
	if age < -15*time.Second {
		return providerpolicy.ErrInvalidResponse
	}
	e.mu.Lock()
	previous := e.health.height
	// Concurrent requests may complete out of order. Small sampled tip changes
	// are tolerated; canonical block verification still checks immutable data.
	regressed := previous-height > maxHeadSkew
	if age <= maxAge && !regressed {
		e.health.height = max(previous, height)
	}
	if age > maxAge || regressed {
		e.health.recoveryAt = time.Time{}
		e.health.recoveryHeight = 0
	}
	e.mu.Unlock()
	if age > maxAge || regressed {
		e.breaker.lagging(previous, height)
		return errRPCHeadLagging
	}
	return nil
}

func endpointQuarantined(e *rpcEndpoint) bool {
	e.breaker.mu.Lock()
	defer e.breaker.mu.Unlock()
	return e.breaker.state == BreakerLagging || e.breaker.state == BreakerInvalid
}

// A single implausibly high provider cannot quarantine all other sources.
// Relative lag requires two independent observations ahead of the candidate;
// timestamp freshness and regression are checked on every actual head read.
func (p *rpcGeneration) quarantineRelativeLag(heads map[*rpcEndpoint]*BlockInfo) {
	type observed struct {
		endpoint *rpcEndpoint
		height   int64
	}
	var ordered []observed
	for e, head := range heads {
		ordered = append(ordered, observed{e, head.BlockHeader.RawData.Number})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].height > ordered[j].height })
	groups := make(map[string]bool)
	var floor int64
	for _, item := range ordered {
		if item.endpoint.trustGroup == "" || groups[item.endpoint.trustGroup] {
			continue
		}
		groups[item.endpoint.trustGroup] = true
		if len(groups) == 2 {
			floor = item.height
			break
		}
	}
	for _, item := range ordered {
		if floor-item.height > maxHeadSkew {
			item.endpoint.breaker.lagging(floor, item.height)
			item.endpoint.mu.Lock()
			item.endpoint.health.recoveryAt = time.Time{}
			item.endpoint.health.recoveryHeight = 0
			item.endpoint.mu.Unlock()
			delete(heads, item.endpoint)
		}
	}
}

func (p *rpcGeneration) recoverEndpoint(ctx context.Context, e *rpcEndpoint, head *BlockInfo) {
	now, height := p.now(), head.BlockHeader.RawData.Number
	e.mu.Lock()
	previous, at := e.health.recoveryHeight, e.health.recoveryAt
	if at.IsZero() {
		e.health.recoveryHeight, e.health.recoveryAt = height, now
	}
	e.mu.Unlock()
	if at.IsZero() || now.Sub(at) < headRecoveryInterval || height <= previous {
		return
	}
	recovery := context.WithValue(ctx, recoveryEndpointKey{}, e)
	if _, err := p.completeEndpointBlock(recovery, e, height); err != nil {
		e.mu.Lock()
		e.health.recoveryAt = time.Time{}
		e.health.recoveryHeight = 0
		e.mu.Unlock()
		return
	}
	e.breaker.success()
	e.mu.Lock()
	e.health.recoveryAt = time.Time{}
	e.health.recoveryHeight = 0
	e.mu.Unlock()
}
