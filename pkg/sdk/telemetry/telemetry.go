// Package telemetry propagates traces and business identifiers.
package telemetry

import (
	"context"

	core "github.com/alex-bogatiuk/metapus/internal/platform/telemetry"
)

func BusinessIDs(ctx context.Context, fallback string) core.BusinessContext {
	return core.BusinessIDs(ctx, fallback)
}

// ConfigureTracing installs W3C trace propagation even when exporting is
// disabled. OTLP exporter settings come from standard OpenTelemetry env vars.
func ConfigureTracing(ctx context.Context, serviceName string, serviceVersion string, getenv func(string) string) (func(context.Context) error, error) {
	return core.ConfigureTracing(ctx, serviceName, serviceVersion, getenv)
}

func ContinueTrace(ctx context.Context, traceparent string) context.Context {
	return core.ContinueTrace(ctx, traceparent)
}

func Traceparent(ctx context.Context) string { return core.Traceparent(ctx) }

func WithBusinessContext(ctx context.Context, correlationID string, causationID string) context.Context {
	return core.WithBusinessContext(ctx, correlationID, causationID)
}
