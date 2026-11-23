package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type nativeBlockResult struct {
	block *types.Block
	err   error
}

// Fetch workers own no DB state. Each height owns one bounded result slot;
// only this ordered consumer captures events and advances the checkpoint.
func (w *Watcher) pollNativeWindow(ctx context.Context, state *crypto.WatcherState, sink crypto.BlockchainEventSink, end, head, finalized int64) error {
	attrs := metric.WithAttributes(attribute.String("crypto.network.code", w.network.Code), attribute.String("crypto.watcher.key", w.watcherKey))
	start := state.LastBlock + 1
	if start > end {
		return nil
	}
	fetchCtx, cancel := context.WithCancel(ctx)
	results := make([]chan nativeBlockResult, int(end-start+1))
	// Heights close to head stay on the ordinary path: another provider may
	// not have them yet or may still follow a competing short fork.
	deepEnd := min(end, head-nativeLaneTipDistance)
	deep := make(chan int64, len(results))
	tip := make(chan int64, len(results))
	for i := range results {
		results[i] = make(chan nativeBlockResult, 1)
		if height := start + int64(i); height <= deepEnd {
			deep <- height
		} else {
			tip <- height
		}
	}
	close(deep)
	close(tip)
	var lanes []blockLane
	if source, ok := w.rpc.(blockLaneSource); ok && deepEnd >= start {
		lanes = source.blockLanes()
	}
	deliver := func(height int64, block *types.Block, err error) bool {
		if err == nil {
			err = w.validateNativeBlock(block, height)
		}
		bytes := int64(0)
		if block != nil {
			bytes = int64(block.Size())
		}
		evmOperationalMetrics.prefetchBytes.Add(ctx, bytes, attrs)
		select {
		case results[height-start] <- nativeBlockResult{block, err}:
			return true
		case <-fetchCtx.Done():
			evmOperationalMetrics.prefetchBytes.Add(ctx, -bytes, attrs)
			return false
		}
	}
	ordinary := func(height int64) (*types.Block, error) {
		return w.rpc.BlockByNumber(fetchCtx, big.NewInt(height))
	}
	var workers sync.WaitGroup
	for range min(w.settings.NativeConcurrency, len(results)) {
		workers.Go(func() {
			for _, queue := range []<-chan int64{deep, tip} {
				for {
					var height int64
					var ok bool
					select {
					case <-fetchCtx.Done():
						return
					case height, ok = <-queue:
					}
					if !ok {
						break
					}
					block, err := ordinary(height)
					if !deliver(height, block, err) {
						return
					}
				}
			}
		})
	}
	laneStates := make([]*nativeLaneState, len(lanes))
	for i, lane := range lanes {
		state := &nativeLaneState{width: lane.width}
		laneStates[i] = state
		for range lane.width {
			workers.Go(func() { w.runNativeLane(fetchCtx, lane, state, deep, ordinary, deliver) })
		}
	}
	defer func() {
		cancel()
		workers.Wait()
		for i, lane := range lanes {
			lane.finish(laneStates[i].outcome())
		}
		for _, slot := range results {
			select {
			case r := <-slot:
				if r.block != nil {
					evmOperationalMetrics.prefetchBytes.Add(ctx, -int64(r.block.Size()), attrs)
				}
			default:
			}
		}
	}()
	for height := start; height <= end; height++ {
		var result nativeBlockResult
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result = <-results[height-start]:
		}
		if result.block != nil {
			evmOperationalMetrics.prefetchBytes.Add(ctx, -int64(result.block.Size()), attrs)
		}
		if result.err != nil {
			return fmt.Errorf("fetch EVM block %d: %w", height, result.err)
		}
		header := result.block.Header()
		if state.LastBlock > 0 && state.Fingerprint != "" && !strings.EqualFold(header.ParentHash.Hex(), state.Fingerprint) {
			// Cancel the old window. The next poll re-reads the checkpoint before
			// invoking the existing common-ancestor/finality workflow.
			return fmt.Errorf("EVM block %d parent differs from committed checkpoint", height)
		}
		if err := w.renewLease(ctx, state); err != nil {
			return err
		}
		confirmations := int(head - height + 1)
		if err := w.processNativeBlock(ctx, state, sink, height, result.block, confirmations, finalityFor(w.network, height, confirmations, finalized)); err != nil {
			return err
		}
		if err := w.commitBlock(ctx, state, header); err != nil {
			return err
		}
	}
	return nil
}

// nativeLaneTipDistance keeps the newest heights of a window on the ordinary
// read path; only heights at least this far below head are spread over lanes.
const nativeLaneTipDistance = 4

type blockLaneSource interface {
	blockLanes() []blockLane
}

type nativeLaneState struct {
	width   int
	served  atomic.Int64
	retired atomic.Int64
	busy    atomic.Bool
	failed  atomic.Bool
}

func (s *nativeLaneState) outcome() blockLaneOutcome {
	return blockLaneOutcome{Served: int(s.served.Load()), Survivors: s.width - int(s.retired.Load()), Busy: s.busy.Load(), Failed: s.failed.Load()}
}

// runNativeLane reads deep heights from one provider. A height the lane cannot
// serve is fetched through the ordinary path, so no height waits for a lane
// twice. Admission pressure retires this worker only; a provider failure
// closes the whole lane for the rest of the window.
func (w *Watcher) runNativeLane(ctx context.Context, lane blockLane, state *nativeLaneState, deep <-chan int64, ordinary func(int64) (*types.Block, error), deliver func(int64, *types.Block, error) bool) {
	for !state.failed.Load() {
		var height int64
		var ok bool
		select {
		case <-ctx.Done():
			return
		case height, ok = <-deep:
		}
		if !ok {
			return
		}
		block, err := lane.read(ctx, height)
		if err == nil {
			state.served.Add(1)
			if !deliver(height, block, nil) {
				return
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}
		busy := errors.Is(err, ErrRPCAdmissionBusy)
		reason := "failure"
		if busy {
			reason = "admission_busy"
			state.busy.Store(true)
		} else {
			state.failed.Store(true)
		}
		evmOperationalMetrics.laneFallbacks.Add(ctx, 1, metric.WithAttributes(attribute.String("crypto.network.code", w.network.Code), attribute.String("crypto.rpc.provider", lane.provider), attribute.String("crypto.reason", reason)))
		block, err = ordinary(height)
		if !deliver(height, block, err) {
			return
		}
		if busy {
			state.retired.Add(1)
			return
		}
	}
}

func (w *Watcher) validateNativeBlock(block *types.Block, height int64) error {
	if block == nil || block.Number().Sign() < 0 || !block.Number().IsInt64() || block.Number().Int64() != height {
		return fmt.Errorf("EVM full block height differs from request %d", height)
	}
	if types.DeriveSha(block.Transactions(), trie.NewStackTrie(nil)) != block.TxHash() || types.CalcUncleHash(block.Uncles()) != block.UncleHash() {
		return fmt.Errorf("EVM full block %d body does not match header roots", height)
	}
	expected, ok := new(big.Int).SetString(w.network.NetworkIdentifier, 10)
	if !ok {
		return fmt.Errorf("invalid EVM chain ID")
	}
	for _, tx := range block.Transactions() {
		// Unprotected legacy transactions predate chain IDs. Protected transfers
		// must match this network, including transfers to unmonitored addresses.
		if tx.Protected() && tx.ChainId().Cmp(expected) != 0 {
			return fmt.Errorf("EVM block %d contains wrong-chain transaction", height)
		}
	}
	return nil
}
func (w *Watcher) commitBlock(ctx context.Context, state *crypto.WatcherState, header *types.Header) error {
	candidate := *state
	candidate.LastBlock, candidate.LastTimestamp, candidate.Fingerprint = header.Number.Int64(), int64(header.Time), header.Hash().Hex()
	candidate.UpdatedAt = time.Now().UTC()
	err := w.committer.CommitBlock(ctx, crypto.WatcherBlockCommit{Candidate: candidate, ExpectedBlock: state.LastBlock, ExpectedHash: state.Fingerprint,
		Block: crypto.WatcherBlockFingerprint{BlockNumber: candidate.LastBlock, BlockHash: candidate.Fingerprint, Timestamp: candidate.LastTimestamp}, RetainFrom: max(candidate.LastBlock-w.lookback, 0)})
	if err != nil {
		return fmt.Errorf("commit EVM watcher block: %w", err)
	}
	*state = candidate
	evmOperationalMetrics.committedBlocks.Add(ctx, 1, metric.WithAttributes(attribute.String("crypto.network.code", w.network.Code), attribute.String("crypto.watcher.key", w.watcherKey)))
	return nil
}
