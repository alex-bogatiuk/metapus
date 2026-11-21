package evm

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// A catch-up window may be spread over providers, one lane per provider. The
// read-order leader keeps the ordinary failover path; each other healthy
// provider contributes one lane pinned to a single endpoint, so a provider
// never receives more than one lane of requests. Bodies are checked against
// their header roots and the parent-hash chain, so the serving provider does
// not change what the watcher accepts.

var errBlockLaneUnavailable = errors.New("EVM block lane unavailable")

type blockLane struct {
	provider string
	width    int
	read     func(context.Context, int64) (*types.Block, error)
	finish   func(blockLaneOutcome)
}

type blockLaneOutcome struct {
	// Served counts blocks the lane delivered itself.
	Served int
	// Survivors counts workers that were not retired by admission pressure.
	Survivors int
	// Busy reports that the provider's pacing could not admit a worker in time.
	Busy bool
	// Failed reports a provider failure; the lane was closed for the window.
	Failed bool
}

// blockLanes returns the lanes available for one window. Lanes start one
// worker wide and widen by one after a clean window that kept every worker
// busy, up to NativeConcurrency;
// admission pressure narrows them to the workers that kept up.
func (p *rpcGeneration) blockLanes() []blockLane {
	if p.hasWrongChainEndpoint() {
		return nil
	}
	maximum := p.settings.NativeConcurrency
	p.readMu.Lock()
	leader := p.preferred[opBlock]
	p.readMu.Unlock()
	if leader == nil || !p.laneReady(leader) {
		leader = nil
		for _, endpoint := range p.endpoints {
			if p.laneReady(endpoint) {
				leader = endpoint
				break
			}
		}
	}
	if leader == nil {
		return nil
	}
	seen := map[string]bool{laneKey(leader): true}
	var lanes []blockLane
	for _, endpoint := range p.endpoints {
		key := laneKey(endpoint)
		if seen[key] || endpoint.quotaLimited || !p.laneReady(endpoint) {
			continue
		}
		seen[key] = true
		endpoint.mu.Lock()
		width := endpoint.blockLaneWidth
		endpoint.mu.Unlock()
		if width < 1 {
			width = 1
		}
		lanes = append(lanes, blockLane{
			provider: endpoint.providerID,
			width:    min(width, maximum),
			read: func(ctx context.Context, height int64) (*types.Block, error) {
				return p.laneBlock(ctx, endpoint, height)
			},
			finish: func(outcome blockLaneOutcome) { finishBlockLane(endpoint, outcome, maximum) },
		})
	}
	return lanes
}

// laneKey groups accounts of one provider, so its pools share a single lane.
func laneKey(endpoint *rpcEndpoint) string {
	if endpoint.trustGroup != "" {
		return "group:" + endpoint.trustGroup
	}
	return "provider:" + endpoint.providerID
}

// laneReady admits only endpoints whose full-block circuit is closed. A
// recovering endpoint is left to the failover path's single probe.
func (p *rpcGeneration) laneReady(endpoint *rpcEndpoint) bool {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if !endpoint.verified || endpoint.state == rpcStateWrongChain || endpoint.unsupportedMethods[string(opBlock)+":"+blockMethod] {
		return false
	}
	c := endpoint.operations[opBlock]
	return c == nil || (!c.Unsupported && !c.Probing && c.OpenUntil.IsZero())
}

func (p *rpcGeneration) laneBlock(ctx context.Context, endpoint *rpcEndpoint, height int64) (*types.Block, error) {
	if !p.beginRead(endpoint, opBlock, time.Now()) {
		return nil, errBlockLaneUnavailable
	}
	block, err := endpointRead(ctx, p, endpoint, opBlock, func(attempt context.Context, client *ethclient.Client) (*types.Block, error) {
		return p.fetchBlock(attempt, client, big.NewInt(height))
	})
	if rpcErrorCategory(err) == "unsupported" {
		endpoint.mu.Lock()
		endpoint.unsupportedMethods[string(opBlock)+":"+blockMethod] = true
		endpoint.mu.Unlock()
	}
	return block, err
}

func finishBlockLane(endpoint *rpcEndpoint, outcome blockLaneOutcome, maximum int) {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	width := min(max(endpoint.blockLaneWidth, 1), maximum)
	switch {
	case outcome.Busy:
		width = max(1, min(outcome.Survivors, width))
	case !outcome.Failed && outcome.Served >= width && width < maximum:
		width++
	}
	endpoint.blockLaneWidth = width
}
