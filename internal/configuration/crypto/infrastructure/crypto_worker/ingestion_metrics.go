package crypto_worker

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type IngestionMonitorMetrics struct {
	quorum                                                                     metric.Int64Gauge
	known                                                                      map[string]map[string]struct{}
	readOK, fullBlockCoverage, lagBlocks, lagSeconds, progressAge, leaseActive metric.Int64Gauge
	inboxPending, inboxProcessing, inboxReview, inboxAge, alertsActive         metric.Int64Gauge
}

func NewIngestionMonitorMetrics() IngestionMonitorMetrics {
	m := otel.Meter("github.com/alex-bogatiuk/metapus/ingestion-monitor")
	gauge := func(name string) metric.Int64Gauge {
		instrument, _ := m.Int64Gauge("crypto.ingestion." + name)
		return instrument
	}
	return IngestionMonitorMetrics{quorum: gauge("quorum_available"), known: make(map[string]map[string]struct{}), readOK: gauge("read_ok"), fullBlockCoverage: gauge("rpc_coverage"), lagBlocks: gauge("lag_blocks"), lagSeconds: gauge("lag_seconds"), progressAge: gauge("last_progress_age_seconds"), leaseActive: gauge("lease_active"), inboxPending: gauge("inbox_pending"), inboxProcessing: gauge("inbox_processing"), inboxReview: gauge("inbox_needs_review"), inboxAge: gauge("inbox_oldest_age_seconds"), alertsActive: gauge("alerts_active")}
}

// Record keeps unknown lag explicit (-1) and publishes zero queue counts after
// recovery, so a completed event cannot leave an old nonzero gauge behind.
func (m *IngestionMonitorMetrics) Record(ctx context.Context, snapshot IngestionMonitorSnapshot, activeAlerts int) {
	boolean := func(value bool) int64 {
		if value {
			return 1
		}
		return 0
	}
	if m.known == nil {
		m.known = make(map[string]map[string]struct{})
	}
	for _, n := range snapshot.Networks {
		if m.known[n.NetworkCode] == nil {
			m.known[n.NetworkCode] = make(map[string]struct{})
		}
		for _, s := range n.Streams {
			m.known[n.NetworkCode][s.WatcherKey] = struct{}{}
		}
	}
	for code := range snapshot.Inbox {
		if m.known[code] == nil {
			m.known[code] = make(map[string]struct{})
		}
	}
	// Reset known series to unknown before publishing this sample, so RPC/DB
	// failure cannot leave the last successful lag masquerading as fresh data.
	for code, keys := range m.known {
		attrs := metric.WithAttributes(attribute.String("crypto.network.code", code))
		m.fullBlockCoverage.Record(ctx, -1, attrs)
		m.quorum.Record(ctx, -1, attrs)
		m.inboxPending.Record(ctx, -1, attrs)
		m.inboxProcessing.Record(ctx, -1, attrs)
		m.inboxReview.Record(ctx, -1, attrs)
		m.inboxAge.Record(ctx, -1, attrs)
		for key := range keys {
			opts := metric.WithAttributes(attribute.String("crypto.network.code", code), attribute.String("crypto.watcher.key", key))
			m.lagBlocks.Record(ctx, -1, opts)
			m.lagSeconds.Record(ctx, -1, opts)
			m.progressAge.Record(ctx, -1, opts)
			m.leaseActive.Record(ctx, -1, opts)
		}
	}
	m.readOK.Record(ctx, boolean(!snapshot.ReadError))
	m.alertsActive.Record(ctx, int64(activeAlerts))
	for _, n := range snapshot.Networks {
		attrs := metric.WithAttributes(attribute.String("crypto.network.code", n.NetworkCode))
		m.fullBlockCoverage.Record(ctx, boolean(n.Head != nil), attrs)
		if n.Cutover != nil && n.Cutover.ActivatedAt != nil {
			m.quorum.Record(ctx, boolean(n.Head != nil), attrs)
		}
		for _, s := range n.Streams {
			opts := metric.WithAttributes(attribute.String("crypto.network.code", n.NetworkCode), attribute.String("crypto.watcher.key", s.WatcherKey))
			m.lagBlocks.Record(ctx, s.LagBlocks, opts)
			m.lagSeconds.Record(ctx, s.LagSeconds, opts)
			m.progressAge.Record(ctx, s.ProgressAgeSeconds, opts)
			m.leaseActive.Record(ctx, boolean(s.LeaseActive), opts)
		}
	}
	for code, inbox := range snapshot.Inbox {
		attrs := metric.WithAttributes(attribute.String("crypto.network.code", code))
		m.inboxPending.Record(ctx, inbox.Pending, attrs)
		m.inboxProcessing.Record(ctx, inbox.Processing, attrs)
		m.inboxReview.Record(ctx, inbox.NeedsReview, attrs)
		m.inboxAge.Record(ctx, inbox.OldestAgeSeconds, attrs)
	}
}
