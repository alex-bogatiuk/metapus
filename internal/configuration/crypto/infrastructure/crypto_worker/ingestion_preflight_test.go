package crypto_worker

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIngestionMissingNativeIsNotHiddenByTokenCheckpoint(t *testing.T) {
	require.False(t, expectedStreamsPresent([]string{"native_eth", "erc20:test"}, []crypto.WatcherState{{WatcherKey: "erc20:test"}}))
	require.True(t, expectedStreamsPresent([]string{"native_eth", "erc20:test"}, []crypto.WatcherState{{WatcherKey: "native_eth"}, {WatcherKey: "erc20:test"}}))
}
func TestIngestionDistinguishesBacklogStallLeaseAndStaleHead(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	head := crypto.IngestionHead{Height: 100, Timestamp: now, ObservedAt: now}
	cp := crypto.IngestionCheckpoint{State: crypto.WatcherState{WatcherKey: "native_eth", LastBlock: 80, LastTimestamp: now.Unix(), UpdatedAt: now, LeaseOwner: "worker"}, LastProgressAt: now, LeaseUntil: &lease}
	hints := crypto.WatcherLagHints{TimeUnit: crypto.CheckpointUnixSeconds, TracksHeadHeight: true}
	require.Equal(t, "catching_up", evaluateIngestionStream(now, "native_eth", head, hints, []crypto.IngestionCheckpoint{cp}).State)
	cp.LastProgressAt = now.Add(-2 * time.Minute)
	require.Equal(t, "checkpoint stalled", evaluateIngestionStream(now, "native_eth", head, hints, []crypto.IngestionCheckpoint{cp}).Reason, "fresh heartbeat cannot hide old progress")
	cp.State.LastBlock = 100
	require.Equal(t, "ready", evaluateIngestionStream(now, "native_eth", head, hints, []crypto.IngestionCheckpoint{cp}).State, "idle at current head is healthy")
	head.ObservedAt = now.Add(-time.Minute)
	require.Equal(t, "stale head", evaluateIngestionStream(now, "native_eth", head, hints, []crypto.IngestionCheckpoint{cp}).Reason)
	head.ObservedAt = now
	cp.LeaseUntil = nil
	require.Equal(t, "watcher lease inactive", evaluateIngestionStream(now, "native_eth", head, hints, []crypto.IngestionCheckpoint{cp}).Reason)
	missing := evaluateIngestionStream(now, "native_eth", head, hints, nil)
	require.Equal(t, "degraded", missing.State)
	require.Equal(t, int64(-1), missing.LagBlocks)
}

func TestIngestionTimestampCursorDoesNotPretendToBeHeadHeight(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	cp := crypto.IngestionCheckpoint{State: crypto.WatcherState{WatcherKey: "native_trx", LastBlock: 1, LastTimestamp: now.UnixMilli(), UpdatedAt: now, LeaseOwner: "worker"}, LastProgressAt: now, LeaseUntil: &lease}
	report := evaluateIngestionStream(now, "native_trx", crypto.IngestionHead{Height: 100000, Timestamp: now, ObservedAt: now}, crypto.WatcherLagHints{TimeUnit: crypto.CheckpointUnixMillis}, []crypto.IngestionCheckpoint{cp})
	require.Equal(t, "ready", report.State)
	require.Equal(t, int64(-1), report.LagBlocks)
}

func TestIngestionUsesExistingFamilyAndPoolLagTolerance(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	cp := crypto.IngestionCheckpoint{State: crypto.WatcherState{WatcherKey: "native_trx", LastTimestamp: now.Add(-134 * time.Second).UnixMilli(), UpdatedAt: now, LeaseOwner: "worker"}, LastProgressAt: now, LeaseUntil: &lease}
	head := crypto.IngestionHead{Height: 100, Timestamp: now.Add(-59 * time.Second), ObservedAt: now}
	report := evaluateIngestionStream(now, "native_trx", head, crypto.WatcherLagHints{TimeUnit: crypto.CheckpointUnixMillis}, []crypto.IngestionCheckpoint{cp}, ingestionLimits{MaxLag: 180 * time.Second, MaxHeadAge: 180 * time.Second})
	require.Equal(t, "ready", report.State, "TRON indexing tolerance must stay compatible with pool policy")
	head.Timestamp = now.Add(-4 * time.Minute)
	require.Equal(t, "stale head", evaluateIngestionStream(now, "native_trx", head, crypto.WatcherLagHints{TimeUnit: crypto.CheckpointUnixMillis}, []crypto.IngestionCheckpoint{cp}, ingestionLimits{MaxLag: 180 * time.Second, MaxHeadAge: 180 * time.Second}).Reason)
}

func TestIngestionAllowsOneBlockSnapshotSkewButRejectsRegressedHead(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	head := crypto.IngestionHead{Height: 100, Timestamp: now, ObservedAt: now}
	cp := crypto.IngestionCheckpoint{State: crypto.WatcherState{WatcherKey: "native_eth", LastBlock: 101, LastTimestamp: now.Unix(), UpdatedAt: now, LeaseOwner: "worker"}, LastProgressAt: now, LeaseUntil: &lease}
	hints := crypto.WatcherLagHints{TimeUnit: crypto.CheckpointUnixSeconds, TracksHeadHeight: true}
	require.Equal(t, "ready", evaluateIngestionStream(now, "native_eth", head, hints, []crypto.IngestionCheckpoint{cp}).State)
	cp.State.LastBlock = 102
	require.Equal(t, "provider head behind checkpoint", evaluateIngestionStream(now, "native_eth", head, hints, []crypto.IngestionCheckpoint{cp}).Reason)
	head.Timestamp = now.Add(-time.Minute)
	require.Equal(t, "stale head", evaluateIngestionStream(now, "native_eth", head, hints, []crypto.IngestionCheckpoint{cp}).Reason)
}
