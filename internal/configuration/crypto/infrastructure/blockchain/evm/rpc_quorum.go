package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// minQuorumHedgeDelay keeps a fast provider's latency estimate from turning
// every ordinary scheduling jitter into an extra request.
const minQuorumHedgeDelay = 50 * time.Millisecond

// CanonicalHeaderByNumber obtains two independent observations when
// configured. It asks exactly as many trust groups as votes are missing, in
// priority order. A failed read is replaced at once; a read that runs past its
// hedge delay (twice the endpoint's header latency, bounded by half the
// attempt timeout) gets one more independent group alongside it. Low-quota
// endpoints only replace votes the ordinary endpoints could not supply and
// never serve as hedges. Every vote received must agree: a disagreement is an
// error, so callers retry rather than classify a financially relevant receipt
// as orphaned from one stale response. Once the votes are in, outstanding
// reads are cancelled. A one-provider Sepolia setup remains supported.
func (p *rpcGeneration) CanonicalHeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, p.settings.ReadBudget())
	defer cancel()
	if number == nil || number.Sign() < 0 {
		return nil, errors.New("canonical EVM header requires a non-negative block number")
	}
	var primary, reserve []*rpcEndpoint
	for endpoint := range votingEndpoints(p.endpoints) {
		if endpoint.quotaLimited {
			reserve = append(reserve, endpoint)
		} else {
			primary = append(primary, endpoint)
		}
	}
	required := min(p.independentProviders(), 2)
	q := &canonicalQuorum{p: p, number: number, primary: primary, reserve: reserve, required: required,
		used: map[*rpcEndpoint]bool{}, observed: map[string]bool{}, inFlight: map[string]bool{}, hedgeAt: map[*rpcEndpoint]time.Time{},
		votes: make(chan headerVote, len(primary)+len(reserve))}
	header, err := q.run(ctx)
	if err != nil {
		return nil, err
	}
	if required == 0 || len(q.observed) < required {
		return nil, fmt.Errorf("only %d/%d required providers returned canonical block %s", len(q.observed), required, number.String())
	}
	return header, nil
}

type headerVote struct {
	endpoint *rpcEndpoint
	header   *types.Header
	err      error
}

// canonicalQuorum is owned by one CanonicalHeaderByNumber call; only its run
// loop touches the maps, read goroutines report through votes.
type canonicalQuorum struct {
	p                *rpcGeneration
	number           *big.Int
	primary, reserve []*rpcEndpoint
	required         int
	used             map[*rpcEndpoint]bool
	observed         map[string]bool
	inFlight         map[string]bool
	hedgeAt          map[*rpcEndpoint]time.Time
	noHedge          bool // No group is left to hedge with until a vote frees one.
	votes            chan headerVote
	reads            sync.WaitGroup
}

func (q *canonicalQuorum) run(ctx context.Context) (*types.Header, error) {
	readCtx, cancelReads := context.WithCancel(ctx)
	defer q.reads.Wait()
	defer cancelReads()
	hedge := time.NewTimer(time.Hour)
	defer hedge.Stop()
	var canonical *types.Header
	for {
		wanted := q.required - len(q.observed)
		if wanted <= 0 {
			return canonical, nil
		}
		for len(q.hedgeAt) < wanted && q.launch(readCtx, false) {
		}
		if len(q.hedgeAt) == 0 {
			return canonical, nil // The caller reports the missing votes.
		}
		q.armHedge(hedge, wanted)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case vote := <-q.votes:
			delete(q.hedgeAt, vote.endpoint)
			delete(q.inFlight, vote.endpoint.trustGroup)
			q.noHedge = false
			if vote.err != nil {
				continue
			}
			if canonical == nil {
				canonical = vote.header
			} else if canonical.Hash() != vote.header.Hash() {
				recordRPCDivergence(ctx, q.p.networkCode, "canonical_block")
				return nil, fmt.Errorf("%w on block %s", ErrRPCProviderDivergence, q.number.String())
			}
			q.observed[vote.endpoint.trustGroup] = true
		case <-hedge.C:
			if q.fresh(time.Now()) >= wanted {
				continue
			}
			if !q.launch(readCtx, true) {
				q.noHedge = true
				continue
			}
			evmOperationalMetrics.quorumHedges.Add(ctx, 1, metric.WithAttributes(attribute.String("crypto.network.code", q.p.networkCode)))
		}
	}
}

// launch starts one read from the next trust group that has neither voted nor
// a read in flight. A hedge never spends a low-quota reserve.
func (q *canonicalQuorum) launch(ctx context.Context, hedge bool) bool {
	pools := [][]*rpcEndpoint{q.primary}
	if !hedge {
		pools = append(pools, q.reserve)
	}
	for _, pool := range pools {
		for _, endpoint := range pool {
			group := endpoint.trustGroup
			if q.used[endpoint] || q.observed[group] || q.inFlight[group] {
				continue
			}
			q.used[endpoint] = true
			if !q.p.beginRead(endpoint, opHeader, time.Now()) {
				continue
			}
			q.inFlight[group] = true
			q.hedgeAt[endpoint] = time.Now().Add(q.p.hedgeDelay(endpoint))
			q.reads.Go(func() {
				header, err := endpointRead(ctx, q.p, endpoint, opHeader, func(attempt context.Context, client *ethclient.Client) (*types.Header, error) {
					return q.p.readHeader(attempt, client, q.number)
				})
				q.votes <- headerVote{endpoint: endpoint, header: header, err: err}
			})
			return true
		}
	}
	return false
}

// fresh counts reads still within their hedge delay.
func (q *canonicalQuorum) fresh(now time.Time) int {
	count := 0
	for _, at := range q.hedgeAt {
		if now.Before(at) {
			count++
		}
	}
	return count
}

// armHedge fires at once when fewer reads are within their hedge delay than
// votes are missing, otherwise at the earliest deadline still ahead.
func (q *canonicalQuorum) armHedge(timer *time.Timer, wanted int) {
	timer.Stop()
	if q.noHedge {
		return
	}
	now := time.Now()
	if q.fresh(now) < wanted {
		timer.Reset(0)
		return
	}
	var next time.Time
	for _, at := range q.hedgeAt {
		if at.After(now) && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	if !next.IsZero() {
		timer.Reset(time.Until(next))
	}
}

// hedgeDelay is twice the endpoint's smoothed header latency, between
// minQuorumHedgeDelay and half the attempt timeout. An endpoint without a
// latency sample waits the full half.
func (p *rpcGeneration) hedgeDelay(endpoint *rpcEndpoint) time.Duration {
	ceiling := p.settings.AttemptTimeout() / 2
	endpoint.mu.Lock()
	latency := time.Duration(0)
	if c := endpoint.operations[opHeader]; c != nil {
		latency = c.Latency
	}
	endpoint.mu.Unlock()
	if latency <= 0 {
		return ceiling
	}
	return min(max(2*latency, minQuorumHedgeDelay), ceiling)
}
