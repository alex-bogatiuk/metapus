package crypto_worker

import (
	"context"
	"errors"
	"time"

	"metapus/internal/domain/crypto"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
)

// IngestionObserver owns dedicated read adapters. It never starts watchers,
// startup reconciliation, custody signing or a financial reconciliation lease.
type IngestionObserver struct {
	processor   *CryptoProcessor
	drivers     *crypto.ChainDriverRegistry
	buildErr    error
	networkCode string
}

type IngestionMonitorSnapshot struct {
	IngestionPreflightResult
	Inbox         map[string]crypto.IngestionInboxBacklog        `json:"inbox"`
	AlertSettings map[string]integrations.IngestionAlertSettings `json:"alertSettings,omitempty"`
	ReadError     bool                                           `json:"readError"`
}

func (p *CryptoProcessor) NewIngestionObserver(networkCode string) *IngestionObserver {
	return &IngestionObserver{processor: p, networkCode: networkCode}
}

func (o *IngestionObserver) Read(ctx context.Context) (IngestionMonitorSnapshot, error) {
	snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{ObservedAt: time.Now().UTC(), Networks: []IngestionNetworkReport{}}, Inbox: make(map[string]crypto.IngestionInboxBacklog)}
	networks, err := o.processor.loadNetworksFiltered(ctx, o.networkCode == "", o.networkCode)
	if err != nil {
		snapshot.ReadError = true
		return snapshot, err
	}
	if len(networks) == 0 {
		snapshot.ReadError = true
		return snapshot, errors.New("no active networks for ingestion observer")
	}
	snapshot.AlertSettings, err = integration_repo.LoadIngestionAlertSettings(ctx)
	if err != nil {
		snapshot.ReadError = true
		return snapshot, err
	}
	refresh := o.drivers == nil
	if !refresh {
		for _, n := range networks {
			if _, err := o.drivers.Resolve(n.ref.ID); err != nil {
				refresh = true
				break
			}
		}
	}
	if refresh {
		closeIngestionDrivers(o.drivers)
		refs := make([]crypto.NetworkRef, 0, len(networks))
		for _, n := range networks {
			refs = append(refs, n.ref)
		}
		o.drivers, o.buildErr = o.processor.cfg.DriverRegistryBuilder.Build(ctx, refs, o.processor.walletDerivers)
	}
	if o.drivers == nil {
		snapshot.ReadError = true
		return snapshot, errors.New("ingestion read adapters unavailable")
	}
	snapshot.IngestionPreflightResult, err = o.processor.readIngestion(ctx, networks, o.drivers, o.buildErr)
	// A per-network adapter error is represented in the report. Preserve it while
	// independently reading the inbox so RPC downtime cannot hide queued payments.
	backlogs, backlogErr := crypto_repo.NewChainEventInboxRepo().ListIngestionBacklogs(ctx)
	if backlogErr != nil {
		snapshot.ReadError = true
		return snapshot, backlogErr
	}
	for _, n := range networks {
		snapshot.Inbox[n.ref.Code] = backlogs[n.ref.ID]
	}
	if err != nil {
		snapshot.ReadError = true
	}
	// Cached adapters may have endpoints that failed identity verification at
	// construction. Refresh them only after a failed head/full-block read, so
	// a recovered endpoint is usable on the next tick without resetting the
	// operation circuits or treating a header probe as full-block recovery.
	for _, report := range snapshot.Networks {
		if report.Head != nil || report.State == "paused" {
			continue
		}
		for _, network := range networks {
			if network.ref.Code != report.NetworkCode {
				continue
			}
			if driver, resolveErr := o.drivers.Resolve(network.ref.ID); resolveErr == nil {
				probeIngestionRecovery(ctx, driver)
			}
		}
	}
	return snapshot, err
}

func probeIngestionRecovery(ctx context.Context, driver crypto.NetworkDriver) {
	if prober, ok := driver.(crypto.DriverRuntimeProber); ok && ctx.Err() == nil {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		prober.ProbeRuntime(probeCtx)
	}
}

func (o *IngestionObserver) Close() { closeIngestionDrivers(o.drivers) }

func closeIngestionDrivers(drivers *crypto.ChainDriverRegistry) {
	if drivers == nil {
		return
	}
	for _, driver := range drivers.Drivers() {
		if closer, ok := driver.(interface{ Close() }); ok {
			closer.Close()
		}
	}
}

// IngestionConditions reports only known evidence. Unknown streams do not
// accidentally resolve an already open incident when RPC/DB reads fail. Shadow
// backlog includes healthy lag samples; the application alert policy classifies
// those measurements independently of diagnostic catching_up/ready state.
func IngestionConditions(snapshot IngestionMonitorSnapshot) map[string]*crypto.IngestionCondition {
	result := make(map[string]*crypto.IngestionCondition)
	add := func(scope, network, key, severity, reason string) {
		result[scope] = &crypto.IngestionCondition{Scope: scope, NetworkCode: network, WatcherKey: key, Severity: severity, Reason: reason}
	}
	if snapshot.ReadError {
		add("monitor", "", "", "error", "read_failure")
		return result
	}
	result["monitor"] = nil
	for _, network := range snapshot.Networks {
		networkScope := "network/" + network.NetworkCode
		result[networkScope] = nil
		evidenceScope := networkScope + "/block_evidence"
		result[evidenceScope] = nil
		if network.Cutover != nil && network.Cutover.Mode == "shadow" && network.Cutover.ShadowPaused {
			for _, scope := range []string{networkScope, evidenceScope, networkScope + "/tron:shadow"} {
				add(scope, network.NetworkCode, "tron:shadow", "info", "operator paused")
			}
			continue
		}
		if network.Cutover != nil && network.Cutover.LastErrorCode != "" {
			severity := "error"
			if network.Cutover.LastErrorStream == "tron:latest" || network.Cutover.LastErrorStream == "tron:shadow" {
				severity = "warning"
			}
			add(evidenceScope, network.NetworkCode, network.Cutover.LastErrorStream, severity, network.Cutover.LastErrorCode)
		}
		if network.Head == nil && network.State != "ready" {
			add(networkScope, network.NetworkCode, "", "error", "rpc_coverage")
		}
		staleHead := false
		for _, stream := range network.Streams {
			if stream.Reason == "stale head" && !stream.Optional {
				staleHead = true
				break
			}
		}
		if staleHead {
			add(networkScope, network.NetworkCode, "", "error", "stale head")
			continue
		}
		for _, stream := range network.Streams {
			scope := networkScope + "/" + stream.WatcherKey
			result[scope] = nil
			shadowLag := stream.Optional && stream.WatcherKey == "tron:shadow" && (stream.State == "ready" || stream.State == "catching_up")
			measuredReady := stream.State == "ready" && (!stream.Optional || stream.WatcherKey == "tron:latest")
			if stream.State == "ready" && !shadowLag && !measuredReady {
				continue
			}
			severity, reason := "error", stream.Reason
			if stream.Optional {
				severity = "warning"
			}
			if stream.State == "catching_up" || shadowLag || measuredReady {
				severity, reason = "warning", "backlog"
			}
			add(scope, network.NetworkCode, stream.WatcherKey, severity, reason)
			if stream.LagSeconds >= 0 || measuredReady {
				lag := stream.LagSeconds
				result[scope].LagSeconds = &lag
			} else if shadowLag {
				// Missing lag is unknown, never evidence of recovery.
				delete(result, scope)
				continue
			}
			if stream.ProgressAgeSeconds >= 0 {
				age := stream.ProgressAgeSeconds
				result[scope].ProgressAgeSeconds = &age
			}
		}
	}
	for code, inbox := range snapshot.Inbox {
		scope := "inbox/" + code
		result[scope] = nil
		if inbox.NeedsReview > 0 {
			add(scope, code, "", "error", "needs_review")
		} else if inbox.OldestAgeSeconds > 60 {
			add(scope, code, "", "error", "inbox_stalled")
		}
	}
	return result
}
