package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type businessContextKey struct{}

type BusinessContext struct {
	CorrelationID string
	CausationID   string
}

func WithBusinessContext(ctx context.Context, correlationID, causationID string) context.Context {
	return context.WithValue(ctx, businessContextKey{}, BusinessContext{
		CorrelationID: correlationID, CausationID: causationID,
	})
}

func BusinessIDs(ctx context.Context, fallback string) BusinessContext {
	if value, ok := ctx.Value(businessContextKey{}).(BusinessContext); ok {
		if value.CorrelationID == "" {
			value.CorrelationID = fallback
		}
		if value.CausationID == "" {
			value.CausationID = fallback
		}
		return value
	}
	return BusinessContext{CorrelationID: fallback, CausationID: fallback}
}

func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

func ContinueTrace(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
}
