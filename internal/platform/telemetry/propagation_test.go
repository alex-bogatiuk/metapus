package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceparentRoundTripAcrossDurableBoundary(t *testing.T) {
	_, err := ConfigureTracing(context.Background(), "test", "dev", func(string) string { return "" })
	require.NoError(t, err)
	traceID, err := trace.TraceIDFromHex("11111111111111111111111111111111")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("2222222222222222")
	require.NoError(t, err)
	started := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	header := Traceparent(started)
	require.Equal(t, "00-11111111111111111111111111111111-2222222222222222-01", header)
	continued := ContinueTrace(context.Background(), header)
	require.Equal(t, traceID, trace.SpanContextFromContext(continued).TraceID())
}

func TestBusinessIDsFillEmptyLegacyContext(t *testing.T) {
	ctx := WithBusinessContext(context.Background(), "", "existing-cause")
	require.Equal(t, BusinessContext{CorrelationID: "payment-123", CausationID: "existing-cause"},
		BusinessIDs(ctx, "payment-123"))
	ctx = WithBusinessContext(context.Background(), "", "")
	require.Equal(t, BusinessContext{CorrelationID: "payment-123", CausationID: "payment-123"},
		BusinessIDs(ctx, "payment-123"))
}
