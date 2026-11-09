package evm

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"metapus/internal/domain/crypto"
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
	jobs := make(chan int64, len(results))
	for i := range results {
		results[i] = make(chan nativeBlockResult, 1)
		jobs <- start + int64(i)
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(w.settings.NativeConcurrency, len(results)) {
		workers.Go(func() {
			for {
				select {
				case <-fetchCtx.Done():
					return
				case height, ok := <-jobs:
					if !ok {
						return
					}
					block, err := w.rpc.BlockByNumber(fetchCtx, big.NewInt(height))
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
					case <-fetchCtx.Done():
						evmOperationalMetrics.prefetchBytes.Add(ctx, -bytes, attrs)
						return
					}
				}
			}
		})
	}
	defer func() {
		cancel()
		workers.Wait()
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
