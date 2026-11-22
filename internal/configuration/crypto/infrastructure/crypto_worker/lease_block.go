package crypto_worker

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/id"
)

const _leaseBlockCheckPeriod = 20 * time.Second

func (p *CryptoProcessor) runWatcherLagLeaseBlockLoop(ctx context.Context) {
	ticker := time.NewTicker(_leaseBlockCheckPeriod)
	defer ticker.Stop()
	p.reconcileWatcherLagLeaseBlocks(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.reconcileWatcherLagLeaseBlocks(ctx)
		}
	}
}

func (p *CryptoProcessor) reconcileWatcherLagLeaseBlocks(ctx context.Context) {
	if p.recorder != nil {
		p.recorder.RecordIfWork(ctx, "crypto.watcher.lease_block", "crypto", func(ctx context.Context) (int, error) {
			return p.reconcileWatcherLagLeaseBlocksOnce(ctx)
		})
		return
	}
	if _, err := p.reconcileWatcherLagLeaseBlocksOnce(ctx); err != nil {
		p.log.Errorw("watcher lag lease-block reconcile failed", "error", err)
	}
}

func (p *CryptoProcessor) reconcileWatcherLagLeaseBlocksOnce(ctx context.Context) (int, error) {
	// Probe financial stream coverage before opening the DB transaction.
	strongReadiness := map[id.ID]bool{}
	if p.drivers != nil {
		networks, err := p.loadNetworks(ctx)
		if err != nil {
			return 0, err
		}
		var selected []networkInfo
		for _, network := range networks {
			driver, err := p.drivers.Resolve(network.ref.ID)
			if err == nil {
				if verifier, ok := driver.(crypto.TransferVerifier); ok && verifier.VerificationRequired() {
					selected = append(selected, network)
				}
			}
		}
		if len(selected) > 0 {
			report, err := p.readIngestion(ctx, selected, p.drivers, nil)
			if err != nil {
				return 0, err
			}
			for i, network := range selected {
				strongReadiness[network.ref.ID] = report.Networks[i].State == "ready"
			}
		}
	}
	changed := 0
	err := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		poolRepo := crypto_repo.NewWalletPoolRepo()
		states, err := poolRepo.ListStates(txCtx)
		if err != nil {
			return err
		}
		networks, err := p.loadNetworks(txCtx)
		if err != nil {
			return err
		}
		assets := make(map[id.ID][]crypto.AssetRef, len(networks))
		for _, network := range networks {
			assets[network.ref.ID] = network.assets
		}
		watchers := crypto_repo.NewWatcherStateRepo()
		now := time.Now().UTC()
		for _, state := range states {
			policy := state.Policy
			if policy.MaxWatcherLagSeconds <= 0 {
				continue
			}
			threshold := time.Duration(policy.MaxWatcherLagSeconds) * time.Second
			checkpoints, err := watchers.ListByNetwork(txCtx, policy.NetworkID)
			if err != nil {
				return err
			}
			if p.drivers == nil {
				if err := poolRepo.SetWatcherLagLeaseBlock(txCtx, policy.NetworkID, true); err != nil {
					return err
				}
				if policy.LeaseBlockedReason != crypto.LeaseBlockedReasonWatcherLag {
					changed++
				}
				continue
			}
			driver, resolveErr := p.drivers.Resolve(policy.NetworkID)
			if resolveErr != nil {
				if err := poolRepo.SetWatcherLagLeaseBlock(txCtx, policy.NetworkID, true); err != nil {
					return err
				}
				if policy.LeaseBlockedReason != crypto.LeaseBlockedReasonWatcherLag {
					changed++
				}
				continue
			}
			blocked := len(checkpoints) == 0
			requiredKeys := map[string]bool{}
			if streams, ok := driver.(crypto.WatcherStreamDescriptor); ok {
				var required []string
				for _, key := range streams.ExpectedWatcherKeys(assets[policy.NetworkID]) {
					if reader, ok := driver.(crypto.StreamIngestionReader); ok && !reader.StreamRequired(key) {
						continue
					}
					required = append(required, key)
					requiredKeys[key] = true
				}
				blocked = !expectedStreamsPresent(required, checkpoints)
			}
			if ready, strong := strongReadiness[policy.NetworkID]; strong {
				blocked = !ready
			}
			descriptor, hasDescriptor := driver.(crypto.WatcherLagDescriptor)
			for _, checkpoint := range checkpoints {
				if len(requiredKeys) > 0 && !requiredKeys[checkpoint.WatcherKey] {
					continue
				}
				if _, strong := strongReadiness[policy.NetworkID]; strong {
					continue
				}
				hints := driver.Capabilities().LagHints()
				if hasDescriptor {
					hints = descriptor.LagHints(checkpoint.WatcherKey)
				}
				lag := crypto.WatcherCheckpointLag(now, hints, checkpoint)
				cryptoMetrics.watcherLagSeconds.Record(txCtx, int64(lag.Seconds()), metric.WithAttributes(
					attribute.String("network_id", policy.NetworkID.String()),
					attribute.String("watcher_key", checkpoint.WatcherKey),
				))
				if lag > threshold {
					blocked = true
				}
			}
			if err := poolRepo.SetWatcherLagLeaseBlock(txCtx, policy.NetworkID, blocked); err != nil {
				return err
			}
			if blocked != (policy.LeaseBlockedReason == crypto.LeaseBlockedReasonWatcherLag) {
				changed++
			}
			blockedVal := int64(0)
			if blocked {
				blockedVal = 1
			}
			netAttr := metric.WithAttributes(attribute.String("network_id", policy.NetworkID.String()))
			cryptoMetrics.walletReady.Record(txCtx, int64(state.Stats.Ready), netAttr)
			cryptoMetrics.walletCooldown.Record(txCtx, int64(state.Stats.Cooldown), netAttr)
			cryptoMetrics.leaseBlocked.Record(txCtx, blockedVal, metric.WithAttributes(
				attribute.String("network_id", policy.NetworkID.String()),
				attribute.String("reason", crypto.LeaseBlockedReasonWatcherLag),
			))
		}
		return nil
	})
	return changed, err
}
