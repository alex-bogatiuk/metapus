package evm

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"metapus/pkg/sdk/id"
)

var evmOperationalMetrics = newEVMOperationalMetrics()

type evmMetrics struct {
	rpcInFlight       metric.Int64UpDownCounter
	rpcAdmission      metric.Float64Histogram
	rpcFailover       metric.Int64Counter
	quorumHedges      metric.Int64Counter
	rpcCircuit        metric.Int64Gauge
	rpcResponseBytes  metric.Int64Counter
	prefetchBytes     metric.Int64UpDownCounter
	laneFallbacks     metric.Int64Counter
	committedBlocks   metric.Int64Counter
	rpcDuration       metric.Float64Histogram
	rpcErrors         metric.Int64Counter
	rpcDivergence     metric.Int64Counter
	watcherLagBlocks  metric.Int64Histogram
	checkpointRewinds metric.Int64Counter
	nonceDivergence   metric.Int64Counter
}

func newEVMOperationalMetrics() evmMetrics {
	meter := otel.Meter("metapus/evm")
	rpcDivergence, _ := meter.Int64Counter("crypto.evm.rpc.divergence")
	watcherLagBlocks, _ := meter.Int64Histogram("crypto.evm.watcher.lag_blocks")
	checkpointRewinds, _ := meter.Int64Counter("crypto.evm.watcher.checkpoint_rewinds")
	nonceDivergence, _ := meter.Int64Counter("crypto.evm.nonce.divergence")
	inflight, _ := meter.Int64UpDownCounter("crypto.evm.rpc.in_flight")
	admission, _ := meter.Float64Histogram("crypto.evm.rpc.admission_wait", metric.WithUnit("s"))
	failover, _ := meter.Int64Counter("crypto.evm.rpc.failover")
	quorumHedges, _ := meter.Int64Counter("crypto.evm.rpc.quorum_hedges")
	circuit, _ := meter.Int64Gauge("crypto.evm.rpc.circuit_open")
	responseBytes, _ := meter.Int64Counter("crypto.evm.rpc.response_bytes", metric.WithUnit("By"))
	prefetch, _ := meter.Int64UpDownCounter("crypto.evm.watcher.prefetch_bytes", metric.WithUnit("By"))
	blocks, _ := meter.Int64Counter("crypto.evm.watcher.committed_blocks")
	laneFallbacks, _ := meter.Int64Counter("crypto.evm.watcher.prefetch_lane_fallbacks")
	return evmMetrics{rpcInFlight: inflight, rpcAdmission: admission, rpcFailover: failover, quorumHedges: quorumHedges, rpcCircuit: circuit, rpcResponseBytes: responseBytes, prefetchBytes: prefetch, laneFallbacks: laneFallbacks, committedBlocks: blocks,
		rpcDuration: mustRPCDuration(meter), rpcErrors: mustRPCErrors(meter),
		rpcDivergence: rpcDivergence, watcherLagBlocks: watcherLagBlocks,
		checkpointRewinds: checkpointRewinds, nonceDivergence: nonceDivergence,
	}
}

func recordRPCDivergence(ctx context.Context, networkCode, observation string) {
	evmOperationalMetrics.rpcDivergence.Add(ctx, 1, metric.WithAttributes(
		attribute.String("crypto.network.code", networkCode),
		attribute.String("crypto.observation", observation),
	))
}

func recordWatcherLag(ctx context.Context, networkID id.ID, watcherKey string, lag int64) {
	evmOperationalMetrics.watcherLagBlocks.Record(ctx, max(lag, 0), metric.WithAttributes(
		attribute.String("crypto.network.id", networkID.String()),
		attribute.String("crypto.watcher.key", watcherKey),
	))
}

func recordCheckpointRewind(ctx context.Context, networkID id.ID, watcherKey string, depth int64, deep bool) {
	evmOperationalMetrics.checkpointRewinds.Add(ctx, 1, metric.WithAttributes(
		attribute.String("crypto.network.id", networkID.String()),
		attribute.String("crypto.watcher.key", watcherKey),
		attribute.Int64("crypto.reorg.depth", max(depth, 0)),
		attribute.Bool("crypto.reorg.deep", deep),
	))
}

func recordNonceDivergence(ctx context.Context, networkID id.ID, reason string) {
	evmOperationalMetrics.nonceDivergence.Add(ctx, 1, metric.WithAttributes(
		attribute.String("crypto.network.id", networkID.String()),
		attribute.String("crypto.reason", reason),
	))
}

func mustRPCDuration(meter metric.Meter) metric.Float64Histogram {
	h, _ := meter.Float64Histogram("crypto.evm.rpc.duration", metric.WithUnit("s"))
	return h
}
func mustRPCErrors(meter metric.Meter) metric.Int64Counter {
	c, _ := meter.Int64Counter("crypto.evm.rpc.errors")
	return c
}
func recordRPCRead(ctx context.Context, network, provider, op string, elapsed time.Duration, category string) {
	attrs := metric.WithAttributes(attribute.String("crypto.network.code", network), attribute.String("crypto.rpc.provider", provider), attribute.String("crypto.rpc.operation", op), attribute.String("crypto.rpc.result", category))
	evmOperationalMetrics.rpcDuration.Record(ctx, elapsed.Seconds(), attrs)
	if category != "" {
		evmOperationalMetrics.rpcErrors.Add(ctx, 1, attrs)
	}
}

func rpcMetricAttrs(network, provider, op string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("crypto.network.code", network), attribute.String("crypto.rpc.provider", provider), attribute.String("crypto.rpc.operation", op))
}
