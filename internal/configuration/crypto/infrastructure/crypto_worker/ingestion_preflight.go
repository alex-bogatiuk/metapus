package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/providerpolicy"
)

type IngestionStreamReport struct {
	Optional           bool                  `json:"optional,omitempty"`
	Head               *crypto.IngestionHead `json:"head,omitempty"`
	WatcherKey         string                `json:"watcherKey"`
	State              string                `json:"state"`
	Reason             string                `json:"reason,omitempty"`
	Checkpoint         *crypto.WatcherState  `json:"checkpoint,omitempty"`
	LagBlocks          int64                 `json:"lagBlocks"`
	LagSeconds         int64                 `json:"lagSeconds"`
	ProgressAgeSeconds int64                 `json:"progressAgeSeconds"`
	LeaseActive        bool                  `json:"leaseActive"`
}
type IngestionNetworkReport struct {
	RPCErrorCode  string                            `json:"rpcErrorCode,omitempty"`
	ElapsedMS     int64                             `json:"elapsedMs"`
	Cutover       *crypto.IngestionCutover          `json:"cutover,omitempty"`
	NetworkCode   string                            `json:"networkCode"`
	State         string                            `json:"state"`
	Reason        string                            `json:"reason,omitempty"`
	Head          *crypto.IngestionHead             `json:"head,omitempty"`
	Streams       []IngestionStreamReport           `json:"streams"`
	RPCOperations []crypto.IngestionOperationStatus `json:"rpcOperations,omitempty"`
}
type IngestionPreflightResult struct {
	ObservedAt time.Time                `json:"observedAt"`
	Ready      bool                     `json:"ready"`
	Networks   []IngestionNetworkReport `json:"networks"`
}

// This path builds read adapters only: no startup nonce reconciliation,
// financial reconciliation lease, custody signing, or persistent mutation.
func (p *CryptoProcessor) RunIngestionPreflight(ctx context.Context, networkCode string) (IngestionPreflightResult, error) {
	result := IngestionPreflightResult{ObservedAt: time.Now().UTC(), Ready: true, Networks: []IngestionNetworkReport{}}
	networks, err := p.loadNetworksFiltered(ctx, networkCode == "", networkCode)
	if err != nil {
		return result, err
	}
	if len(networks) == 0 {
		return result, errors.New("no networks found for ingestion preflight")
	}
	refs := make([]crypto.NetworkRef, 0, len(networks))
	for _, n := range networks {
		refs = append(refs, n.ref)
	}
	if p.cfg.DriverRegistryBuilder == nil || p.walletDerivers == nil {
		return result, errors.New("ingestion driver builder/derivers unavailable")
	}
	drivers, buildErr := p.cfg.DriverRegistryBuilder.Build(ctx, refs, p.walletDerivers)
	if drivers == nil {
		return result, buildErr
	}
	defer closeIngestionDrivers(drivers)
	return p.readIngestion(ctx, networks, drivers, buildErr)
}

func (p *CryptoProcessor) readIngestion(ctx context.Context, networks []networkInfo, drivers *crypto.ChainDriverRegistry, buildErr error) (IngestionPreflightResult, error) {
	result := IngestionPreflightResult{ObservedAt: time.Now().UTC(), Ready: true, Networks: []IngestionNetworkReport{}}
	lagLimits, err := ingestionLagLimits(ctx)
	if err != nil {
		return result, err
	}
	reports, readErr := readIngestionNetworks(ctx, networks, func(ctx context.Context, network networkInfo) (IngestionNetworkReport, error) {
		return p.readNetworkIngestion(ctx, network, drivers, lagLimits[network.ref.ID])
	})
	result.Networks = reports
	for _, report := range reports {
		if report.State != "ready" && report.State != "paused" {
			result.Ready = false
		}
	}
	if readErr != nil {
		return result, readErr
	}
	if buildErr != nil {
		return result, fmt.Errorf("build ingestion adapters: %w", buildErr)
	}
	return result, nil
}

// ingestionLagLimits returns each network's wallet-pool watcher lag allowance.
func ingestionLagLimits(ctx context.Context) (map[id.ID]time.Duration, error) {
	poolStates, err := crypto_repo.NewWalletPoolRepo().ListStates(ctx)
	if err != nil {
		return nil, err
	}
	lagLimits := make(map[id.ID]time.Duration, len(poolStates))
	for _, state := range poolStates {
		lagLimits[state.Policy.NetworkID] = time.Duration(state.Policy.MaxWatcherLagSeconds) * time.Second
	}
	return lagLimits, nil
}

// Each network owns its report and RPC deadline. Slow providers in one network
// cannot consume another network's sample budget; all workers join on shutdown.
func readIngestionNetworks(ctx context.Context, networks []networkInfo, read func(context.Context, networkInfo) (IngestionNetworkReport, error)) ([]IngestionNetworkReport, error) {
	reports := make([]IngestionNetworkReport, len(networks))
	failures := make([]error, len(networks))
	jobs := make(chan int, len(networks))
	for i := range networks {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(4, len(networks)) {
		workers.Go(func() {
			for i := range jobs {
				started := time.Now()
				reports[i], failures[i] = read(ctx, networks[i])
				reports[i].ElapsedMS = time.Since(started).Milliseconds()
			}
		})
	}
	workers.Wait()
	return reports, errors.Join(failures...)
}

func (p *CryptoProcessor) readNetworkIngestion(ctx context.Context, network networkInfo, drivers *crypto.ChainDriverRegistry, maxLag time.Duration) (IngestionNetworkReport, error) {
	report := IngestionNetworkReport{NetworkCode: network.ref.Code, State: "degraded", Streams: []IngestionStreamReport{}}
	driver, resolveErr := drivers.Resolve(network.ref.ID)
	if resolveErr != nil {
		report.Reason = "driver unavailable"
		return report, nil
	}
	descriptor, ok := driver.(crypto.WatcherStreamDescriptor)
	reader, hasReader := driver.(crypto.ChainIngestionReader)
	if !ok || !hasReader {
		report.Reason = "ingestion capability unavailable"
		return report, nil
	}
	expectedKeys := descriptor.ExpectedWatcherKeys(network.assets)
	if _, ok := driver.(crypto.NetworkWatcherFactory); ok {
		cutover, cutErr := crypto_repo.NewBlockIngestionRepo().Cutover(ctx, network.ref.ID)
		if cutErr == nil {
			report.Cutover = &cutover
		} else if !errors.Is(cutErr, crypto.ErrIngestionCutoverNotFound) {
			return report, cutErr
		}
	}
	if report.Cutover != nil && report.Cutover.Mode == "shadow" && report.Cutover.ShadowPaused {
		checkpoints, err := crypto_repo.NewWatcherStateRepo().ListIngestionCheckpoints(ctx, network.ref.ID)
		if err != nil {
			return report, err
		}
		report.State = "paused"
		stream := IngestionStreamReport{Optional: true, WatcherKey: "tron:shadow", State: "paused", Reason: "operator paused", LagBlocks: -1, LagSeconds: -1, ProgressAgeSeconds: -1}
		for _, cp := range checkpoints {
			if cp.State.WatcherKey == "tron:shadow" {
				state := cp.State
				stream.Checkpoint = &state
				stream.LeaseActive = state.LeaseOwner != "" && cp.LeaseUntil != nil && cp.LeaseUntil.After(time.Now())
			}
		}
		report.Streams = append(report.Streams, stream)
		return report, nil
	}
	head, streamHeads, headErr := readIngestionHeads(ctx, reader, driver, network.assets, expectedKeys)
	report.RPCErrorCode = ingestionRPCErrorCode(headErr)
	if operations, ok := driver.(crypto.IngestionOperationReporter); ok {
		report.RPCOperations = operations.IngestionOperations()
	}
	streamReader, hasStreamReader := driver.(crypto.StreamIngestionReader)
	if headErr != nil && !hasStreamReader {
		report.Reason = "ingestion RPC coverage unavailable"
		return report, nil
	}
	if headErr == nil {
		report.Head = &head
	}
	checkpoints, readErr := crypto_repo.NewWatcherStateRepo().ListIngestionCheckpoints(ctx, network.ref.ID)
	if readErr != nil {
		return report, readErr
	}
	report.State = "ready"
	for _, key := range expectedKeys {
		streamHead, streamErr, optional := head, headErr, false
		if hasStreamReader {
			optional = !streamReader.StreamRequired(key)
			if strings.HasPrefix(key, "tron:") {
				if key == "tron:solidified" {
					streamHead, streamErr = head, headErr
				} else {
					observed := streamHeads[key]
					streamHead, streamErr = observed.head, observed.err
				}
			}
		}
		if streamErr != nil {
			stream := IngestionStreamReport{WatcherKey: key, Optional: optional, State: "degraded", Reason: "ingestion RPC coverage unavailable", LagBlocks: -1, LagSeconds: -1, ProgressAgeSeconds: -1}
			report.Streams = append(report.Streams, stream)
			if !optional {
				report.State = "degraded"
				report.Reason = stream.Reason
			}
			continue
		}
		hints := driver.Capabilities().LagHints()
		if specific, ok := driver.(crypto.WatcherLagDescriptor); ok {
			hints = specific.LagHints(key)
		}
		limits := ingestionLimits{MaxLag: max(60*time.Second, maxLag, time.Duration(driver.Capabilities().MinWatcherLagSeconds)*time.Second), MaxHeadAge: max(30*time.Second, time.Duration(driver.Capabilities().MinWatcherLagSeconds)*time.Second)}
		if strings.HasPrefix(key, "tron:") {
			limits = ingestionLimits{MaxLag: 60 * time.Second, MaxHeadAge: 90 * time.Second}
			if key == "tron:latest" {
				limits.MaxHeadAge = 30 * time.Second
			}
		}
		stream := evaluateIngestionStream(time.Now().UTC(), key, streamHead, hints, checkpoints, limits)
		stream.Optional, stream.Head = optional, &streamHead
		report.Streams = append(report.Streams, stream)
		if optional {
			continue
		}
		if stream.State == "degraded" {
			report.State = "degraded"
		} else if stream.State == "catching_up" && report.State == "ready" {
			report.State = "catching_up"
		}
	}
	return report, nil
}

type ingestionHeadRead struct {
	head crypto.IngestionHead
	err  error
}

func readIngestionHeads(ctx context.Context, reader crypto.ChainIngestionReader, driver crypto.NetworkDriver, assets []crypto.AssetRef, keys []string) (crypto.IngestionHead, map[string]ingestionHeadRead, error) {
	var primary ingestionHeadRead
	streamReads := make([]ingestionHeadRead, len(keys))
	var workers sync.WaitGroup
	workers.Go(func() { primary.head, primary.err = reader.ReadIngestionHead(ctx, assets) })
	specific, ok := driver.(crypto.StreamIngestionReader)
	if ok {
		for i, key := range keys {
			if key != "tron:latest" {
				continue
			}
			workers.Go(func() { streamReads[i].head, streamReads[i].err = specific.ReadStreamHead(ctx, key, assets) })
		}
	}
	workers.Wait()
	heads := make(map[string]ingestionHeadRead, len(keys))
	for i, key := range keys {
		heads[key] = streamReads[i]
		if key == "tron:shadow" || key == "tron:solidified" {
			heads[key] = primary
		}
	}
	return primary.head, heads, primary.err
}

// Never persist transport bodies or authenticated URLs as diagnostic text.
func ingestionRPCErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, crypto.ErrChainQuorumUnavailable):
		return "quorum_unavailable"
	case errors.Is(err, crypto.ErrVerificationMismatch):
		return "provider_disagreement"
	case errors.Is(err, providerpolicy.ErrAdmissionBusy):
		return "admission_busy"
	case errors.Is(err, providerpolicy.ErrRateLimited):
		return "rate_limited"
	default:
		return "rpc_unavailable"
	}
}

type ingestionLimits struct{ MaxLag, MaxHeadAge time.Duration }

func evaluateIngestionStream(now time.Time, key string, head crypto.IngestionHead, hints crypto.WatcherLagHints, checkpoints []crypto.IngestionCheckpoint, policy ...ingestionLimits) IngestionStreamReport {
	limits := ingestionLimits{MaxLag: 60 * time.Second, MaxHeadAge: 30 * time.Second}
	if len(policy) > 0 {
		limits = policy[0]
	}
	report := IngestionStreamReport{WatcherKey: key, State: "degraded", Reason: "missing checkpoint", LagBlocks: -1, LagSeconds: -1, ProgressAgeSeconds: -1}
	for _, checkpoint := range checkpoints {
		if checkpoint.State.WatcherKey != key {
			continue
		}
		state := checkpoint.State
		report.Checkpoint = &state
		report.LeaseActive = state.LeaseOwner != "" && checkpoint.LeaseUntil != nil && checkpoint.LeaseUntil.After(now)
		if hints.TracksHeadHeight {
			report.LagBlocks = max(head.Height-state.LastBlock, 0)
		}
		report.LagSeconds = int64(crypto.WatcherCheckpointLag(now, hints, state).Seconds())
		if strings.HasPrefix(key, "tron:") {
			report.LagSeconds = max(head.Timestamp.UnixMilli()-state.LastTimestamp, 0) / 1000
		}
		report.ProgressAgeSeconds = int64(max(now.Sub(checkpoint.LastProgressAt), 0).Seconds())
		switch {
		case head.ObservedAt.IsZero() || now.Sub(head.ObservedAt) > 30*time.Second || head.Timestamp.IsZero() || now.Sub(head.Timestamp) > limits.MaxHeadAge:
			report.Reason = "stale head"
		case !report.LeaseActive:
			report.Reason = "watcher lease inactive"
			// Head and checkpoint are read separately while the worker advances.
		// A one-block difference is within a fresh snapshot; larger regression is not.
		case hints.TracksHeadHeight && state.LastBlock > head.Height+1:
			report.Reason = "provider head behind checkpoint"
		case (report.LagBlocks > 2 || report.LagSeconds > int64(limits.MaxLag.Seconds())) && report.ProgressAgeSeconds > 60:
			report.Reason = "checkpoint stalled"
		case hints.TracksHeadHeight && report.LagBlocks > 2:
			report.State = "catching_up"
			report.Reason = "backlog"
		case report.LagSeconds > int64(limits.MaxLag.Seconds()):
			report.State = "catching_up"
			report.Reason = "timestamp backlog"
		default:
			report.State = "ready"
			report.Reason = ""
		}
		return report
	}
	return report
}
func expectedStreamsPresent(keys []string, checkpoints []crypto.WatcherState) bool {
	if len(keys) == 0 {
		return false
	}
	found := make(map[string]bool, len(checkpoints))
	for _, state := range checkpoints {
		found[state.WatcherKey] = true
	}
	for _, key := range keys {
		if !found[key] {
			return false
		}
	}
	return true
}
