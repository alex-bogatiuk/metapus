package crypto_worker

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

const networkRuntimeStatusPeriod = 30 * time.Second

type resourceProviderStatusReporter interface {
	ResourceProviderStatus(ctx context.Context, networkID id.ID) (ready bool, state string)
	ResourceProviderStatuses(ctx context.Context, networkID id.ID) []crypto.RuntimeEndpointStatus
}

func (p *CryptoProcessor) runNetworkRuntimeStatusLoop(ctx context.Context) {
	p.publishNetworkRuntimeStatus(ctx)
	ticker := time.NewTicker(networkRuntimeStatusPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.publishNetworkRuntimeStatus(ctx)
		}
	}
}

func (p *CryptoProcessor) publishNetworkRuntimeStatus(ctx context.Context) {
	if p.drivers == nil {
		return
	}
	inputs := p.loadRuntimeIngestionInputs(ctx)
	for _, driver := range p.drivers.Drivers() {
		network := driver.Network()
		if prober, ok := driver.(crypto.DriverRuntimeProber); ok {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			prober.ProbeRuntime(probeCtx)
			cancel()
		}
		status := crypto.NetworkRuntimeStatus{
			NetworkID: network.ID, ObservedAt: time.Now().UTC(), WorkerID: p.workerID,
			ResourceProviderState: "unconfigured",
		}
		if reporter, ok := driver.(crypto.DriverRuntimeReporter); ok {
			status.RPCEndpoints = reporter.RuntimeEndpoints()
		}
		if _, err := resolveOutboundSigner(ctx, p.familySigners, p.authorizations, network); err == nil {
			status.SignerReady = true
		} else {
			// Do not persist Vault paths, tenant-scoped errors, or authorization
			// metadata. Detailed readiness failures stay in controlled traces.
			status.SignerError = "signer readiness check failed"
		}
		if reporter, ok := resolveResourcePreparer(p.resourcePreparers, network.Family).(resourceProviderStatusReporter); ok {
			status.ResourceProviders = reporter.ResourceProviderStatuses(ctx, network.ID)
			status.ResourceProviderReady, status.ResourceProviderState = summarizeResourceProviderStatuses(status.ResourceProviders)
		}
		status.Ingestion = p.readRuntimeIngestion(ctx, network, inputs)
		recordRuntimeReadiness(ctx, network, "signer", status.SignerReady)
		recordRuntimeReadiness(ctx, network, "resource_provider", status.ResourceProviderReady)
		if err := persistNetworkRuntimeStatus(ctx, status); err != nil {
			p.log.Warnw("failed to persist crypto network runtime status", "network_id", status.NetworkID, "error", err)
		}
	}
}

func persistNetworkRuntimeStatus(ctx context.Context, status crypto.NetworkRuntimeStatus) error {
	return crypto_repo.NewWorkerRuntimeRepo().PersistNetworkRuntimeStatus(ctx, status)
}

type runtimeIngestionInputs struct {
	assets    map[id.ID][]crypto.AssetRef
	lagLimits map[id.ID]time.Duration
}

func (p *CryptoProcessor) loadRuntimeIngestionInputs(ctx context.Context) *runtimeIngestionInputs {
	networks, err := p.loadNetworksFiltered(ctx, true, "")
	if err != nil {
		p.log.Warnw("failed to load networks for ingestion runtime status", "error", err)
		return nil
	}
	lagLimits, err := ingestionLagLimits(ctx)
	if err != nil {
		p.log.Warnw("failed to load watcher lag limits for ingestion runtime status", "error", err)
		return nil
	}
	inputs := &runtimeIngestionInputs{assets: make(map[id.ID][]crypto.AssetRef, len(networks)), lagLimits: lagLimits}
	for _, n := range networks {
		inputs.assets[n.ref.ID] = n.assets
	}
	return inputs
}

// readRuntimeIngestion evaluates the network's watcher streams with the same
// rules as the ingestion preflight and monitor, so the Admin page and alerts
// never disagree about whether a stream is behind.
func (p *CryptoProcessor) readRuntimeIngestion(ctx context.Context, network crypto.NetworkRef, inputs *runtimeIngestionInputs) *crypto.NetworkIngestionStatus {
	if inputs == nil {
		return nil
	}
	assets, watched := inputs.assets[network.ID]
	if !watched {
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	report, err := p.readNetworkIngestion(readCtx, networkInfo{ref: network, assets: assets}, p.drivers, inputs.lagLimits[network.ID])
	if err != nil {
		p.log.Warnw("failed to read ingestion runtime status", "network_id", network.ID, "error", err)
		return nil
	}
	return runtimeIngestionStatus(report)
}

func runtimeIngestionStatus(report IngestionNetworkReport) *crypto.NetworkIngestionStatus {
	status := &crypto.NetworkIngestionStatus{
		State: report.State, Reason: report.Reason, RPCErrorCode: report.RPCErrorCode,
		Streams: make([]crypto.IngestionStreamStatus, 0, len(report.Streams)),
	}
	for _, stream := range report.Streams {
		item := crypto.IngestionStreamStatus{
			WatcherKey: stream.WatcherKey, Optional: stream.Optional, State: stream.State, Reason: stream.Reason,
			LagBlocks: stream.LagBlocks, LagSeconds: stream.LagSeconds, ProgressAgeSeconds: stream.ProgressAgeSeconds,
			LeaseActive: stream.LeaseActive,
		}
		if stream.Checkpoint != nil {
			block := stream.Checkpoint.LastBlock
			item.LastBlock, item.BlockHash = &block, stream.Checkpoint.Fingerprint
		}
		if stream.Head != nil && stream.Head.Height > 0 {
			height := stream.Head.Height
			item.HeadHeight = &height
		}
		status.Streams = append(status.Streams, item)
	}
	return status
}
