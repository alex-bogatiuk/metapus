package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// ConfigureTracing installs W3C trace propagation even when exporting is
// disabled. OTLP exporter settings come from standard OpenTelemetry env vars.
func ConfigureTracing(ctx context.Context, serviceName, serviceVersion string, getenv func(string) string) (func(context.Context) error, error) {
	if getenv == nil {
		return nil, fmt.Errorf("telemetry environment reader is required")
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	if strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")) == "" &&
		strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		return func(context.Context) error { return nil }, nil
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exporter, err := otlptracegrpc.New(connectCtx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	serviceResource, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL, semconv.ServiceNamespace("metapus"),
		semconv.ServiceName(serviceName), semconv.ServiceVersion(serviceVersion),
	))
	if err != nil {
		_ = exporter.Shutdown(context.Background())
		return nil, fmt.Errorf("build tracing resource: %w", err)
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(serviceResource))
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}
