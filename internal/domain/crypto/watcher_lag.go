package crypto

import "time"

// WatcherLagHints is family-neutral clock metadata for one watcher stream.
// Adapters that have per-stream indexing delay implement WatcherLagDescriptor
// and override the capability defaults.
type WatcherLagHints struct {
	TimeUnit         CheckpointTimeUnit
	IndexingLag      time.Duration
	TracksHeadHeight bool
}

// WatcherLagDescriptor lets a driver report timestamp units and indexing delay
// for a specific watcher key without leaking family names into orchestration.
type WatcherLagDescriptor interface {
	LagHints(watcherKey string) WatcherLagHints
}

// WatcherCheckpointLag is the conservative lag used to freeze leasing.
func WatcherCheckpointLag(now time.Time, hints WatcherLagHints, state WatcherState) time.Duration {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	updatedLag := max(now.Sub(state.UpdatedAt), 0)
	tsLag := timestampLag(now, hints, state.LastTimestamp)
	if tsLag > updatedLag {
		return tsLag
	}
	return updatedLag
}

func timestampLag(now time.Time, hints WatcherLagHints, lastTimestamp int64) time.Duration {
	if lastTimestamp <= 0 || !hints.TimeUnit.Valid() {
		return 0
	}
	var checkpoint time.Time
	switch hints.TimeUnit {
	case CheckpointUnixMillis:
		checkpoint = time.UnixMilli(lastTimestamp)
	case CheckpointUnixSeconds:
		checkpoint = time.Unix(lastTimestamp, 0)
	default:
		return 0
	}
	lag := now.Sub(checkpoint)
	if hints.IndexingLag > 0 {
		lag -= hints.IndexingLag
	}
	if lag < 0 {
		return 0
	}
	return lag
}

// WatcherStreamDescriptor declares all required streams, independent of which
// checkpoints happen to exist. Orchestration never infers a chain's key format.
type WatcherStreamDescriptor interface {
	ExpectedWatcherKeys(assets []AssetRef) []string
}
