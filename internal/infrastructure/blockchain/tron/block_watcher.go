package tron

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/blockchain/watcherlease"
	"metapus/pkg/logger"
)

const latestWindow = int64(128)
const blockHistoryWindow = int64(512)

type prefetchedBlock struct {
	data    completeBlock
	sources []crypto.VerificationSource
}

type blockWatcher struct {
	driver     *Driver
	cutover    crypto.IngestionCutover
	key        string
	shadow     bool
	latest     bool
	assets     []crypto.AssetRef
	lastAudit  time.Time
	prefetched map[int64]prefetchedBlock
	// backlog reports that the last successful solidified poll ended below the
	// agreed head, so the next window starts without the poll interval.
	backlog bool
}

// solidifiedTipWindow is the window at the agreed head. Behind it the window
// grows to TRONSettings.SolidifiedCatchUpBlocks: a fixed four-block window
// every two seconds commits slower than TRON produces blocks, so an outage
// longer than a few minutes never recovers.
const solidifiedTipWindow = 4

// Targets are refreshed transactionally by CaptureBlock, including when an
// address is issued during the RPC read.
func (*blockWatcher) ReplaceMonitoredAddresses([]string) {}

func (d *Driver) NetworkWatchers(ctx context.Context, network crypto.NetworkRef, assets []crypto.AssetRef, _ []string) ([]crypto.ManagedWatcher, error) {
	mode := d.rpc.settings.IngestionMode
	if mode == "paused" {
		if d.blockRepo == nil {
			return nil, fmt.Errorf("TRON block capture repository unavailable")
		}
		cutover, err := d.blockRepo.Cutover(ctx, network.ID)
		if err != nil {
			return nil, err
		}
		_, err = d.ensureCutover(ctx, network, crypto.WatcherBlockFingerprint{BlockNumber: cutover.Height, BlockHash: cutover.Hash, Timestamp: cutover.Timestamp}, mode, assets)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := d.validateNetwork(network); err != nil {
		return nil, err
	}
	if d.blockRepo == nil {
		return nil, fmt.Errorf("TRON block capture repository unavailable")
	}
	key := "tron:solidified"
	if mode == "shadow" {
		key = "tron:shadow"
	}
	watchers := []crypto.ManagedWatcher{&blockWatcher{driver: d, assets: append([]crypto.AssetRef(nil), assets...), key: key, shadow: mode == "shadow"}}
	if mode == "hybrid" {
		watchers = append(watchers, &blockWatcher{driver: d, assets: append([]crypto.AssetRef(nil), assets...), key: "tron:latest", latest: true})
	}
	return watchers, nil
}

func (w *blockWatcher) Start(ctx context.Context, sink crypto.BlockchainEventSink) error {
	for {
		err := w.run(ctx, sink)
		if !w.shadow || !errors.Is(err, crypto.ErrShadowPaused) {
			return err
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *blockWatcher) run(ctx context.Context, _ crypto.BlockchainEventSink) error {
	cutover, err := w.driver.blockRepo.Cutover(ctx, w.driver.network.ID)
	if err != nil && !errors.Is(err, crypto.ErrIngestionCutoverNotFound) {
		return err
	}
	if w.shadow && cutover.ShadowPaused {
		return crypto.ErrShadowPaused
	}
	baseline := crypto.WatcherBlockFingerprint{BlockNumber: cutover.Height, BlockHash: cutover.Hash, Timestamp: cutover.Timestamp}
	if err != nil || cutover.Mode != w.driver.rpc.settings.IngestionMode {
		head, headErr := w.driver.rpc.QuorumHead(ctx, chainconfig.RoleSolidity)
		if headErr != nil {
			return errors.Join(crypto.ErrChainQuorumUnavailable, headErr)
		}
		baseline = crypto.WatcherBlockFingerprint{BlockNumber: head.BlockHeader.RawData.Number, BlockHash: head.BlockID, Timestamp: head.BlockHeader.RawData.Timestamp}
	}
	w.cutover, err = w.driver.ensureCutover(ctx, w.driver.network, baseline, w.driver.rpc.settings.IngestionMode, w.assets)
	if err != nil {
		return err
	}
	if w.cutover.DecoderVersion != crypto.TRONTransferEvidenceSchema {
		return crypto.ErrDecoderVersionConflict
	}
	leases, ok := w.driver.stateRepo.(crypto.WatcherLeaseRepository)
	if !ok {
		return fmt.Errorf("TRON block watcher requires fencing leases")
	}
	initial := &crypto.WatcherState{NetworkID: w.driver.network.ID, WatcherKey: w.key, LastBlock: w.cutover.Height, LastTimestamp: w.cutover.Timestamp, Fingerprint: w.cutover.Hash}
	state, err := leases.Acquire(ctx, initial, "tron-block:"+id.New().String())
	if err != nil {
		return err
	}
	// Prefetch belongs to this lease acquisition and never survives a restart.
	w.prefetched = nil
	w.lastAudit = time.Time{}
	identity := *state
	workCtx, stop := watcherlease.Start(ctx, 20*time.Second, 5*time.Second, func(renewCtx context.Context) error { return leases.Renew(renewCtx, &identity) })
	defer func() {
		stop()
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := leases.Release(releaseCtx, &identity); err != nil {
			logger.Warn(releaseCtx, "TRON block watcher lease release failed", "watcher_key", w.key, "error", err)
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	pending := map[int64]time.Time{}
	completed := map[int64]string{}
	var seeded bool
	for workCtx.Err() == nil {
		if w.shadow {
			control, err := w.driver.blockRepo.Cutover(workCtx, w.driver.network.ID)
			if err != nil {
				return err
			}
			if control.ShadowPaused {
				return crypto.ErrShadowPaused
			}
		}
		started := time.Now()
		var pollErr error
		if w.latest {
			pollErr = w.pollLatest(workCtx, state, pending, completed, &seeded)
		} else {
			pollErr = w.pollSolidified(workCtx, state)
		}
		if errors.Is(pollErr, crypto.ErrShadowPaused) {
			return pollErr
		}
		w.recordPoll(workCtx, started, pollErr)
		if errors.Is(pollErr, crypto.ErrWatcherLeaseLost) || errors.Is(pollErr, crypto.ErrWatcherCheckpointConflict) {
			return pollErr
		}
		if pollErr != nil && workCtx.Err() == nil {
			if recorder, ok := w.driver.blockRepo.(crypto.IngestionFailureRecorder); ok {
				if err := recorder.ReportIngestionFailure(workCtx, w.driver.network.ID, w.key, state.LastBlock+1, blockErrorCategory(pollErr), false); err != nil {
					logger.Warn(workCtx, "TRON ingestion diagnostics capture failed", "error", err)
				}
			}
			logger.Warn(workCtx, "TRON block ingestion waiting", "network_id", w.driver.network.ID, "watcher_key", w.key, "error_category", blockErrorCategory(pollErr), "error", pollErr)
		}
		if pollErr == nil {
			if recorder, ok := w.driver.blockRepo.(crypto.IngestionFailureRecorder); ok {
				if err := recorder.ReportIngestionFailure(workCtx, w.driver.network.ID, w.key, state.LastBlock, "", false); err != nil {
					logger.Warn(workCtx, "TRON ingestion diagnostics recovery failed", "error", err)
				}
			}
		}
		if pollErr == nil && !w.latest && w.backlog {
			continue // Behind the agreed head: the next window starts at once.
		}
		select {
		case <-workCtx.Done():
		case <-ticker.C:
		}
	}
	if errors.Is(context.Cause(workCtx), crypto.ErrWatcherLeaseLost) {
		return crypto.ErrWatcherLeaseLost
	}
	return ctx.Err()
}

func blockErrorCategory(err error) string {
	switch {
	case semanticReadFailure(err):
		return rpcErrorCategory(err)
	case errors.Is(err, crypto.ErrVerificationMismatch):
		return "provider_disagreement"
	case errors.Is(err, crypto.ErrVerificationPending):
		return "incomplete_receipts"
	case errors.Is(err, crypto.ErrChainQuorumUnavailable):
		return "quorum_unavailable"
	default:
		return rpcErrorCategory(err)
	}
}

func (w *blockWatcher) pollSolidified(ctx context.Context, state *crypto.WatcherState) error {
	pool, release, acquireErr := w.driver.rpc.runtime.Acquire(ctx)
	if acquireErr != nil {
		return acquireErr
	}
	defer release()
	if time.Since(w.lastAudit) >= 30*time.Second {
		actual, err := pool.AgreedBlockHash(ctx, state.LastBlock)
		if err != nil {
			return err
		}
		if !strings.EqualFold(actual, state.Fingerprint) {
			if recorder, ok := w.driver.blockRepo.(crypto.IngestionFailureRecorder); ok {
				if err := recorder.ReportIngestionFailure(ctx, w.driver.network.ID, w.key, state.LastBlock, "provider_disagreement", true); err != nil {
					return err
				}
			}
			return crypto.ErrVerificationMismatch
		}
		w.lastAudit = time.Now()
	}
	window, err := pool.prepareBlockWindow(ctx)
	if err != nil {
		return err
	}
	w.backlog = false
	settings := w.driver.rpc.settings.Defaults()
	head := window.solidified.BlockHeader.RawData.Number
	size := int64(solidifiedTipWindow)
	if head-state.LastBlock > size {
		size = int64(settings.SolidifiedCatchUpBlocks)
	}
	end := min(head, state.LastBlock+size)
	if w.prefetched == nil {
		w.prefetched = make(map[int64]prefetchedBlock, size)
	}
	for height := range w.prefetched {
		if height <= state.LastBlock || height > end {
			delete(w.prefetched, height)
		}
	}
	if end <= state.LastBlock {
		return nil
	}
	type result struct {
		prefetchedBlock
		err error
	}
	first := state.LastBlock + 1
	var missing []int64
	for height := first; height <= end; height++ {
		if _, exists := w.prefetched[height]; !exists {
			missing = append(missing, height)
		}
	}
	results := make([]result, len(missing))
	// Two owned workers, four bounded prefetched heights. Successful results
	// survive an earlier incomplete block so retries only read missing heights.
	// Sources retain their original agreed head: confirmations may undercount
	// until the verifier refreshes them, but can never be artificially raised.
	jobs := make(chan int, len(results))
	for i := range results {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(settings.BackfillConcurrency, len(results)) {
		wg.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					results[i].err = ctx.Err()
					continue
				}
				results[i].data, results[i].sources, results[i].err = pool.readAgreedWindowBlock(ctx, missing[i], window)
			}
		})
	}
	wg.Wait()
	for i, result := range results {
		if result.err == nil {
			w.prefetched[missing[i]] = result.prefetchedBlock
		}
	}
	for height := first; height <= end; height++ {
		result, exists := w.prefetched[height]
		if !exists {
			for i, missingHeight := range missing {
				if missingHeight == height {
					return results[i].err
				}
			}
			return crypto.ErrVerificationPending
		}
		confs := int(result.sources[0].HeadHeight-result.data.Block.BlockHeader.RawData.Number) + 1
		events, err := decodeTransfers(w.driver.network, result.data, crypto.FinalitySolidified, confs)
		if err != nil {
			return err
		}
		if err := w.capture(ctx, state, result.data, events, result.sources, true); err != nil {
			if errors.Is(err, crypto.ErrVerificationMismatch) {
				if recorder, ok := w.driver.blockRepo.(crypto.IngestionFailureRecorder); ok {
					if reportErr := recorder.ReportIngestionFailure(ctx, w.driver.network.ID, w.key, state.LastBlock, "provider_disagreement", true); reportErr != nil {
						return reportErr
					}
				}
			}
			return err
		}
		delete(w.prefetched, height)
	}
	w.backlog = head > state.LastBlock
	return nil
}

func (w *blockWatcher) pollLatest(ctx context.Context, state *crypto.WatcherState, pending map[int64]time.Time, completed map[int64]string, seeded *bool) error {
	head, err := w.driver.rpc.LatestHead(ctx)
	if err != nil {
		return err
	}
	upper := head.BlockHeader.RawData.Number
	lower := max(w.cutover.Height+1, upper-latestWindow+1)
	now := time.Now()
	if !*seeded {
		for height := lower; height <= upper; height++ {
			pending[height] = time.Time{}
		}
		*seeded = true
	}
	for height := max(state.LastBlock+1, lower); height <= upper; height++ {
		pending[height] = time.Time{}
	}
	// Recheck the tip on every poll: same-height replacements are observable.
	if completed[upper] != head.BlockID {
		pending[upper] = time.Time{}
	}
	for height := range pending {
		if height < lower || height > upper {
			delete(pending, height)
		}
	}
	for height := range completed {
		if height < lower {
			delete(completed, height)
		}
	}
	var heights []int64
	for height := upper; height >= lower && len(heights) < 4; height-- {
		if due, exists := pending[height]; exists && !due.After(now) {
			heights = append(heights, height)
		}
	}
	type result struct {
		data completeBlock
		err  error
	}
	results := make([]result, len(heights))
	jobs := make(chan int, len(heights))
	for i := range heights {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(2, len(heights)) {
		wg.Go(func() {
			for i := range jobs {
				results[i].data, results[i].err = w.driver.rpc.ReadLatestBlock(ctx, heights[i])
			}
		})
	}
	wg.Wait()
	var waiting error
	for i, result := range results {
		if result.err != nil {
			waiting = errors.Join(waiting, result.err)
			pending[heights[i]] = now.Add(5 * time.Second)
			continue
		}
		events, err := decodeTransfers(w.driver.network, result.data, crypto.FinalityProvisional, int(upper-heights[i])+1)
		if err != nil {
			waiting = errors.Join(waiting, err)
			pending[heights[i]] = now.Add(5 * time.Second)
			continue
		}
		if err := w.capture(ctx, state, result.data, events, nil, false); err != nil {
			return err
		}
		delete(pending, heights[i])
		completed[heights[i]] = result.data.Block.BlockID
	}
	return waiting
}

func (w *blockWatcher) capture(ctx context.Context, state *crypto.WatcherState, data completeBlock, events []crypto.BlockchainEvent, sources []crypto.VerificationSource, sequential bool) error {
	block := data.Block
	candidate := *state
	if sequential || block.BlockHeader.RawData.Number > state.LastBlock {
		candidate.LastBlock, candidate.LastTimestamp, candidate.Fingerprint = block.BlockHeader.RawData.Number, block.BlockHeader.RawData.Timestamp, block.BlockID
	}
	commit := crypto.WatcherBlockCommit{Candidate: candidate, ExpectedBlock: state.LastBlock, ExpectedHash: state.Fingerprint,
		Block: crypto.WatcherBlockFingerprint{BlockNumber: block.BlockHeader.RawData.Number, BlockHash: block.BlockID, Timestamp: block.BlockHeader.RawData.Timestamp}, RetainFrom: max(candidate.LastBlock-blockHistoryWindow, 0)}
	err := w.driver.blockRepo.CaptureBlock(ctx, crypto.CapturedBlock{DecoderVersion: crypto.TRONTransferEvidenceSchema, Commit: commit, ParentHash: block.BlockHeader.RawData.ParentHash, Events: events, Sources: sources, Shadow: w.shadow, Sequential: sequential})
	if err == nil {
		*state = candidate
		w.recordCommit(ctx, len(events))
	}
	return err
}

var _ crypto.NetworkWatcherFactory = (*Driver)(nil)
