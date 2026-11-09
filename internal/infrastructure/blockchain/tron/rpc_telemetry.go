package tron

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var tronRPCTracer = otel.Tracer("metapus/crypto/tron-rpc")

type tronRPCInstruments struct {
	requests           metric.Int64Counter
	durationMillis     metric.Int64Histogram
	circuitObservation metric.Int64Counter
	ambiguousBroadcast metric.Int64Counter
}

var tronRPCMetrics = newTRONRPCInstruments()

func newTRONRPCInstruments() tronRPCInstruments {
	meter := otel.Meter("metapus/crypto/tron-rpc")
	requests, _ := meter.Int64Counter("crypto.rpc.requests")
	durationMillis, _ := meter.Int64Histogram("crypto.rpc.duration_ms")
	circuitObservation, _ := meter.Int64Counter("crypto.rpc.circuit_state_observations")
	ambiguousBroadcast, _ := meter.Int64Counter("crypto.rpc.broadcast_ambiguous")
	return tronRPCInstruments{
		requests: requests, durationMillis: durationMillis,
		circuitObservation: circuitObservation, ambiguousBroadcast: ambiguousBroadcast,
	}
}

func beginRPCRequest(ctx context.Context, networkCode string, endpoint *rpcEndpoint, operations ...string) (trace.Span, time.Time) {
	attrs := rpcAttributes(networkCode, endpoint)
	if len(operations) > 0 {
		attrs = append(attrs, rpcOperationAttribute(operations[0]))
	}
	_, span := tronRPCTracer.Start(ctx, "crypto.rpc.request", trace.WithAttributes(attrs...))
	return span, time.Now()
}

func finishRPCRequest(ctx context.Context, span trace.Span, started time.Time, networkCode string, endpoint *rpcEndpoint, outcome string, err error, operations ...string) {
	status := endpoint.breaker.snapshot(endpoint.providerID, endpoint.role)
	attrs := append(rpcAttributes(networkCode, endpoint),
		attribute.String("crypto.rpc.outcome", outcome),
		attribute.String("crypto.rpc.circuit_state", string(status.State)),
	)
	if len(operations) > 0 {
		attrs = append(attrs, rpcOperationAttribute(operations[0]))
		endpoint.mu.Lock()
		c := endpoint.operations[operations[0]]
		state := "closed"
		if c != nil {
			if !c.OpenUntil.IsZero() {
				state = "open"
			}
			if c.Probing {
				state = "half_open"
			}
			if c.Unsupported {
				state = "unsupported"
			}
		}
		endpoint.mu.Unlock()
		attrs[len(attrs)-2] = attribute.String("crypto.rpc.circuit_state", state)
	}
	tronRPCMetrics.requests.Add(ctx, 1, metric.WithAttributes(attrs...))
	tronRPCMetrics.durationMillis.Record(ctx, time.Since(started).Milliseconds(), metric.WithAttributes(attrs...))
	tronRPCMetrics.circuitObservation.Add(ctx, 1, metric.WithAttributes(attrs...))
	span.SetAttributes(attrs...)
	if err != nil {
		span.SetStatus(codes.Error, outcome)
	} else {
		span.SetStatus(codes.Ok, outcome)
	}
	span.End()
}

func recordRPCCircuitSkip(ctx context.Context, networkCode string, endpoint *rpcEndpoint) {
	status := endpoint.breaker.snapshot(endpoint.providerID, endpoint.role)
	attrs := append(rpcAttributes(networkCode, endpoint),
		attribute.String("crypto.rpc.outcome", "circuit_skipped"),
		attribute.String("crypto.rpc.circuit_state", string(status.State)),
	)
	tronRPCMetrics.circuitObservation.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func recordAmbiguousRPCBroadcast(ctx context.Context, networkCode string) {
	tronRPCMetrics.ambiguousBroadcast.Add(ctx, 1, metric.WithAttributes(
		attribute.String("crypto.network.code", networkCode),
		attribute.String("crypto.chain.family", "tron"),
	))
}

func rpcAttributes(networkCode string, endpoint *rpcEndpoint) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("crypto.network.code", networkCode),
		attribute.String("crypto.chain.family", "tron"),
		attribute.String("crypto.rpc.role", string(endpoint.role)),
		attribute.String("crypto.rpc.provider_id", endpoint.providerID),
	}
}

func rpcOperationAttribute(op string) attribute.KeyValue {
	return attribute.String("crypto.rpc.operation", op)
}
