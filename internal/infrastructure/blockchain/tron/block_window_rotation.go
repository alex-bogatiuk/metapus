package tron

import (
	"context"
	"strings"
	"sync"

	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/pkg/logger"
)

// maxWindowMembers bounds the independent sources one catch-up window spreads
// over: four members give six pairs, each member serving half the heights.
const maxWindowMembers = 4

type windowCandidate struct {
	source, latest *rpcEndpoint
	joined         bool
}

// extendBlockWindow lets a catch-up window rotate its heights over more than
// one pair. Every block still needs two independent sources, but with three
// members each serves two thirds of the heights instead of all of them. A
// member joins only after its own solidity and full-node endpoints return the
// window's agreed solidified and latest blocks, so every pair compares blocks
// below the same agreed heads and evidence keeps one head per window. A
// candidate that fails or disagrees is left out and the window keeps the
// sources it has; a disagreement is logged, never used as evidence. Shadow
// mode keeps its single pair.
func (p *rpcGeneration) extendBlockWindow(ctx context.Context, window agreedBlockWindow) agreedBlockWindow {
	if p.settings.IngestionMode == "shadow" || len(window.pair) != 2 || len(window.latestPair) != 2 || len(window.pairs) > 0 {
		return window
	}
	taken := []*rpcEndpoint{window.pair[0], window.pair[1]}
	var candidates []*windowCandidate
	for _, endpoint := range p.readOrder(chainconfig.RoleSolidity, opBlock) {
		if len(taken) == maxWindowMembers {
			break
		}
		if !p.independentOfAll(endpoint, taken) {
			continue
		}
		if latest := p.latestCounterpart(endpoint); latest != nil {
			candidates = append(candidates, &windowCandidate{source: endpoint, latest: latest})
			taken = append(taken, endpoint)
		}
	}
	if len(candidates) == 0 {
		return window
	}
	var wg sync.WaitGroup
	for _, c := range candidates {
		wg.Go(func() { c.joined = p.confirmsWindow(ctx, c.source, c.latest, window) })
	}
	wg.Wait()
	members := []*rpcEndpoint{window.pair[0], window.pair[1]}
	latest := []*rpcEndpoint{window.latestPair[0], window.latestPair[1]}
	for _, c := range candidates {
		if c.joined {
			members, latest = append(members, c.source), append(latest, c.latest)
		}
	}
	if len(members) == 2 {
		return window
	}
	for i := range members {
		for j := i + 1; j < len(members); j++ {
			window.pairs = append(window.pairs, windowPair{pair: []*rpcEndpoint{members[i], members[j]}, latestPair: []*rpcEndpoint{latest[i], latest[j]}})
		}
	}
	return window
}

// independentOfAll applies the independent-pair rules of
// independentPairExcept to a candidate against every taken source.
func (p *rpcGeneration) independentOfAll(endpoint *rpcEndpoint, taken []*rpcEndpoint) bool {
	if endpoint.trustGroup == "" || endpointQuarantined(endpoint) {
		return false
	}
	now := p.now()
	endpoint.mu.Lock()
	headCircuit, blockCircuit := endpoint.circuit(opHead), endpoint.circuit(opBlock)
	unavailable := endpoint.unavailableUntil.After(now) || headCircuit.OpenUntil.After(now) || blockCircuit.OpenUntil.After(now) || headCircuit.Unsupported || blockCircuit.Unsupported
	endpoint.mu.Unlock()
	if unavailable {
		return false
	}
	for _, other := range taken {
		if endpoint.trustGroup == other.trustGroup || endpoint.providerID == other.providerID || endpoint.client.baseURL == other.client.baseURL {
			return false
		}
	}
	return true
}

// latestCounterpart is the full-node endpoint of the candidate's trust group
// that would read its latest head, as prepareBlockWindowExcept chooses it.
func (p *rpcGeneration) latestCounterpart(source *rpcEndpoint) *rpcEndpoint {
	for _, endpoint := range p.readOrder(chainconfig.RoleFullNode, opHead) {
		if endpoint.trustGroup == source.trustGroup && !endpointQuarantined(endpoint) {
			return endpoint
		}
	}
	return nil
}

// confirmsWindow reads the window's agreed solidified and latest blocks from
// the candidate's endpoints and accepts it only when both match.
func (p *rpcGeneration) confirmsWindow(ctx context.Context, source, latest *rpcEndpoint, window agreedBlockWindow) bool {
	pairs := []struct {
		endpoint *rpcEndpoint
		agreed   *BlockInfo
	}{{source, window.solidified}, {latest, window.latest}}
	var wg sync.WaitGroup
	matched := make([]bool, len(pairs))
	for i, check := range pairs {
		wg.Go(func() {
			height := check.agreed.BlockHeader.RawData.Number
			block, err := p.endpointBlock(ctx, check.endpoint, height)
			if err != nil {
				return
			}
			if !strings.EqualFold(block.BlockID, check.agreed.BlockID) {
				logger.Warn(ctx, "TRON provider disagrees with the agreed window head; left out of block rotation",
					"network_code", p.networkCode, "provider_id", check.endpoint.providerID, "height", height)
				return
			}
			matched[i] = true
		})
	}
	wg.Wait()
	return matched[0] && matched[1]
}
