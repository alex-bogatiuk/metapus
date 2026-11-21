//go:build integration

package worker

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"metapus/internal/configuration"
	"metapus/internal/configuration/none"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/pkg/logger"
)

type recoveringWorkerRegistry struct {
	tenant.Registry
	entry   *tenant.Tenant
	lookups atomic.Int32
}

func (r *recoveringWorkerRegistry) ListActive(context.Context) ([]*tenant.Tenant, error) {
	return []*tenant.Tenant{r.entry}, nil
}
func (r *recoveringWorkerRegistry) GetByID(context.Context, string) (*tenant.Tenant, error) {
	if r.lookups.Add(1) == 1 {
		return nil, errors.New("temporary registry outage")
	}
	return r.entry, nil
}

func TestWorkerReliabilityTenantStartsAfterTransientInitializationFailure(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is required")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	registry := &recoveringWorkerRegistry{entry: &tenant.Tenant{ID: id.New().String(), DBHost: config.Host, DBPort: int(config.Port), DBName: config.Database, Status: tenant.StatusActive}}
	settings := tenantdb.DefaultManagerConfig()
	settings.DBUser, settings.DBPassword = config.User, config.Password
	settings.MinConnsPerTenant, settings.MaxConnsPerTenant = 0, 10
	settings.HealthCheckPeriod, settings.PoolIdleTimeout = 0, 0
	log, err := logger.New(logger.Config{Level: "error"})
	require.NoError(t, err)
	manager := tenantdb.NewManager(settings, registry, log)
	defer manager.Close()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	defer func() { otel.SetMeterProvider(previous); _ = provider.Shutdown(context.Background()) }()
	// The empty configuration's part: the platform's jobs alone, no Vault.
	part, err := none.New().Worker(t.Context(), configuration.WorkerEnv{Logger: log})
	require.NoError(t, err)
	worker := NewMultiTenantWorker(manager, log, configuration.Compose(none.New()), part)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	require.Eventually(t, func() bool {
		var data metricdata.ResourceMetrics
		if reader.Collect(t.Context(), &data) != nil {
			return false
		}
		for _, scope := range data.ScopeMetrics {
			for _, metric := range scope.Metrics {
				if metric.Name != "worker.component.ready" {
					continue
				}
				gauge, ok := metric.Data.(metricdata.Gauge[int64])
				if !ok {
					continue
				}
				for _, point := range gauge.DataPoints {
					if point.Value == 1 {
						return true
					}
				}
			}
		}
		return false
	}, 10*time.Second, 25*time.Millisecond)
	require.GreaterOrEqual(t, registry.lookups.Load(), int32(2))
	require.Equal(t, 1, manager.Stats().Tenants[0].ActiveRefs)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("tenant worker did not drain")
	}
	require.Zero(t, manager.Stats().Tenants[0].ActiveRefs)
}
