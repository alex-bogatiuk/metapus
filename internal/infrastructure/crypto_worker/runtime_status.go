package crypto_worker

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
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
