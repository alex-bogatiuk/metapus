package telemetry

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"
)

type startupMetricsReceiver struct {
	metricspb.UnimplementedMetricsServiceServer
	requests chan *metricspb.ExportMetricsServiceRequest
}

func (r *startupMetricsReceiver) Export(_ context.Context, request *metricspb.ExportMetricsServiceRequest) (*metricspb.ExportMetricsServiceResponse, error) {
	r.requests <- request
	return &metricspb.ExportMetricsServiceResponse{}, nil
}

type startupTraceReceiver struct {
	tracepb.UnimplementedTraceServiceServer
	requests chan *tracepb.ExportTraceServiceRequest
}

func (r *startupTraceReceiver) Export(_ context.Context, request *tracepb.ExportTraceServiceRequest) (*tracepb.ExportTraceServiceResponse, error) {
	r.requests <- request
	return &tracepb.ExportTraceServiceResponse{}, nil
}

// No-endpoint tests cannot detect schema conflicts introduced by an SDK
// upgrade. Exercise production initialization and real OTLP exports together.
func TestOTLPStartupAndExportUseCompatibleResourceSchema(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	metrics := &startupMetricsReceiver{requests: make(chan *metricspb.ExportMetricsServiceRequest, 1)}
	traces := &startupTraceReceiver{requests: make(chan *tracepb.ExportTraceServiceRequest, 1)}
	metricspb.RegisterMetricsServiceServer(server, metrics)
	tracepb.RegisterTraceServiceServer(server, traces)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	endpoint := "http://" + listener.Addr().String()
	for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"} {
		t.Setenv(key, endpoint)
	}
	for _, key := range []string{"OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_METRICS_HEADERS", "OTEL_EXPORTER_OTLP_TRACES_HEADERS"} {
		t.Setenv(key, "")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "3600000")
	oldMeter, oldTracer, oldPropagator := otel.GetMeterProvider(), otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		otel.SetTracerProvider(oldTracer)
		otel.SetTextMapPropagator(oldPropagator)
	})

	t.Run("metrics", func(t *testing.T) {
		shutdown, err := ConfigureMetrics(t.Context(), "worker", "schema-test", os.Getenv)
		require.NoError(t, err)
		counter, err := otel.Meter("telemetry-startup-test").Int64Counter("startup_schema_check")
		require.NoError(t, err)
		counter.Add(t.Context(), 1)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		require.NoError(t, shutdown(ctx))
		select {
		case request := <-metrics.requests:
			require.NotEmpty(t, request.ResourceMetrics)
			rm := request.ResourceMetrics[0]
			require.Equal(t, resource.Default().SchemaURL(), rm.SchemaUrl)
			assertStartupServiceAttributes(t, rm.Resource.Attributes, "worker")
		case <-ctx.Done():
			t.Fatal("OTLP metrics were not exported")
		}
	})
	t.Run("tracing", func(t *testing.T) {
		shutdown, err := ConfigureTracing(t.Context(), "api", "schema-test", os.Getenv)
		require.NoError(t, err)
		_, span := otel.Tracer("telemetry-startup-test").Start(t.Context(), "startup_schema_check")
		span.End()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		require.NoError(t, shutdown(ctx))
		select {
		case request := <-traces.requests:
			require.NotEmpty(t, request.ResourceSpans)
			rs := request.ResourceSpans[0]
			require.Equal(t, resource.Default().SchemaURL(), rs.SchemaUrl)
			assertStartupServiceAttributes(t, rs.Resource.Attributes, "api")
		case <-ctx.Done():
			t.Fatal("OTLP trace was not exported")
		}
	})
}
func assertStartupServiceAttributes(t *testing.T, attributes []*commonpb.KeyValue, name string) {
	t.Helper()
	got := map[string]string{}
	for _, attribute := range attributes {
		got[attribute.Key] = attribute.Value.GetStringValue()
	}
	require.Equal(t, "metapus", got["service.namespace"])
	require.Equal(t, name, got["service.name"])
	require.Equal(t, "schema-test", got["service.version"])
}
