package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// ConfigureMetrics installs an OTLP/gRPC-backed global meter provider when an
// OTLP metrics endpoint is configured. The exporter intentionally relies on
// the standard OTEL_EXPORTER_OTLP_* environment variables for TLS, headers,
// compression and temporality instead of introducing Metapus-specific secret
// configuration.
func ConfigureMetrics(ctx context.Context, serviceName, serviceVersion string, getenv func(string) string) (func(context.Context) error, error) {
	if getenv == nil {
		return nil, fmt.Errorf("telemetry environment reader is required")
	}
	if strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")) == "" &&
		strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		return func(context.Context) error { return nil }, nil
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exporter, err := otlpmetricgrpc.New(connectCtx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}

	serviceResource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNamespace("metapus"),
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		_ = exporter.Shutdown(context.Background())
		return nil, fmt.Errorf("build telemetry resource: %w", err)
	}

	reader := metric.NewPeriodicReader(exporter)
	provider := metric.NewMeterProvider(metric.WithReader(reader), metric.WithResource(serviceResource))
	otel.SetMeterProvider(provider)
	return provider.Shutdown, nil
}
