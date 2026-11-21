package crypto

import (
	"testing"
	"time"
)

func TestWatcherCheckpointLagSubtractsIndexingDelay(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	state := WatcherState{
		LastTimestamp: now.Add(-3 * time.Minute).UnixMilli(),
		UpdatedAt:     now.Add(-10 * time.Second),
	}
	lag := WatcherCheckpointLag(now, WatcherLagHints{TimeUnit: CheckpointUnixMillis, IndexingLag: 2 * time.Minute}, state)
	if lag < 50*time.Second || lag > 70*time.Second {
		t.Fatalf("indexed lag = %s, want ~1m after 2m indexing delay", lag)
	}
}

func TestWatcherCheckpointLagUsesUnixSeconds(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	state := WatcherState{
		LastTimestamp: now.Add(-45 * time.Second).Unix(),
		UpdatedAt:     now.Add(-5 * time.Second),
	}
	lag := WatcherCheckpointLag(now, WatcherLagHints{TimeUnit: CheckpointUnixSeconds}, state)
	if lag < 40*time.Second || lag > 50*time.Second {
		t.Fatalf("seconds lag = %s, want ~45s from block timestamp", lag)
	}
}

func TestWatcherCheckpointLagUsesUnixMillisWithoutIndexingDelay(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	state := WatcherState{
		LastTimestamp: now.Add(-45 * time.Second).UnixMilli(),
		UpdatedAt:     now.Add(-5 * time.Second),
	}
	lag := WatcherCheckpointLag(now, WatcherLagHints{TimeUnit: CheckpointUnixMillis}, state)
	if lag < 40*time.Second || lag > 50*time.Second {
		t.Fatalf("millis lag = %s, want ~45s from block timestamp", lag)
	}
}
