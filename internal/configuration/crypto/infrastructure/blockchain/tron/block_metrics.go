package tron

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var blockMetrics = func() struct {
	commits, events, errors metric.Int64Counter
	pollSeconds             metric.Float64Histogram
} {
	m := otel.Meter("github.com/alex-bogatiuk/metapus/tron/blocks")
	commits, _ := m.Int64Counter("crypto.tron.block_commits")
	events, _ := m.Int64Counter("crypto.tron.decoded_events")
	errors, _ := m.Int64Counter("crypto.tron.read_failures")
	polls, _ := m.Float64Histogram("crypto.tron.poll_seconds", metric.WithUnit("s"))
	return struct {
		commits, events, errors metric.Int64Counter
		pollSeconds             metric.Float64Histogram
	}{commits, events, errors, polls}
}()

func (w *blockWatcher) recordPoll(ctx context.Context, started time.Time, err error) {
	attrs := []attribute.KeyValue{attribute.String("crypto.network.code", w.driver.network.Code), attribute.String("crypto.watcher.key", w.key)}
	blockMetrics.pollSeconds.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
	if err != nil {
		attrs = append(attrs, attribute.String("error.category", blockErrorCategory(err)))
		blockMetrics.errors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func (w *blockWatcher) recordCommit(ctx context.Context, events int) {
	opts := metric.WithAttributes(attribute.String("crypto.network.code", w.driver.network.Code), attribute.String("crypto.watcher.key", w.key))
	blockMetrics.commits.Add(ctx, 1, opts)
	blockMetrics.events.Add(ctx, int64(events), opts)
}
