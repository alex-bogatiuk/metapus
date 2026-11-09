package crypto_worker

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"metapus/internal/domain/crypto"
)

func TestIngestionMetricsClearRecoveredInboxAndMarkUnavailableEvidenceUnknown(t *testing.T) {
	previous := otel.GetMeterProvider()
	defer otel.SetMeterProvider(previous)
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer func() { _ = provider.Shutdown(t.Context()) }()
	instruments := NewIngestionMonitorMetrics()
	snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{Networks: []IngestionNetworkReport{{NetworkCode: "SEPOLIA", Head: &crypto.IngestionHead{}, Streams: []IngestionStreamReport{{WatcherKey: "native_eth", LagBlocks: 7, LagSeconds: 80, ProgressAgeSeconds: 20, LeaseActive: true}}}}}, Inbox: map[string]crypto.IngestionInboxBacklog{"SEPOLIA": {Pending: 2, OldestAgeSeconds: 90}}}
	value := func(name string) int64 {
		var collected metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(t.Context(), &collected))
		for _, scope := range collected.ScopeMetrics {
			for _, m := range scope.Metrics {
				if m.Name == "crypto.ingestion."+name {
					gauge, ok := m.Data.(metricdata.Gauge[int64])
					require.True(t, ok)
					require.Len(t, gauge.DataPoints, 1)
					return gauge.DataPoints[0].Value
				}
			}
		}
		t.Fatalf("metric %s missing", name)
		return 0
	}
	instruments.Record(t.Context(), snapshot, 0)
	require.Equal(t, int64(7), value("lag_blocks"))
	require.Equal(t, int64(2), value("inbox_pending"))
	snapshot.Networks[0].Head = nil
	snapshot.Networks[0].Streams = nil
	snapshot.Inbox["SEPOLIA"] = crypto.IngestionInboxBacklog{}
	instruments.Record(t.Context(), snapshot, 1)
	require.Equal(t, int64(-1), value("lag_blocks"))
	require.Equal(t, int64(0), value("rpc_coverage"))
	require.Zero(t, value("inbox_pending"))
	require.Zero(t, value("inbox_oldest_age_seconds"))
	instruments.Record(t.Context(), IngestionMonitorSnapshot{ReadError: true}, 1)
	require.Zero(t, value("read_ok"))
	require.Equal(t, int64(-1), value("rpc_coverage"))
	require.Equal(t, int64(-1), value("inbox_pending"))
}
