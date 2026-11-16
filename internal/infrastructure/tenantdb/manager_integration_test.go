//go:build integration

package tenantdb

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/tenant"
	"metapus/pkg/logger"
)

type fixtureRegistry struct {
	tenant.Registry
	entries map[string]*tenant.Tenant
}

func (r fixtureRegistry) GetByID(_ context.Context, id string) (*tenant.Tenant, error) {
	entry, ok := r.entries[id]
	if !ok {
		return nil, tenant.ErrTenantNotFound
	}
	return entry, nil
}

func TestTenantPoolLifecycleConcurrentCreationReferencesAndShutdown(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "explicit disposable PostgreSQL required")
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	log, err := logger.New(logger.Config{Level: "error"})
	require.NoError(t, err)
	registry := fixtureRegistry{entries: map[string]*tenant.Tenant{
		"a":        {ID: "a", DBHost: config.Host, DBPort: int(config.Port), DBName: config.Database, Status: tenant.StatusActive},
		"b":        {ID: "b", DBHost: config.Host, DBPort: int(config.Port), DBName: config.Database, Status: tenant.StatusActive},
		"disabled": {ID: "disabled", Status: tenant.StatusSuspended},
	}}
	settings := DefaultManagerConfig()
	settings.DBUser, settings.DBPassword = config.User, config.Password
	settings.MinConnsPerTenant, settings.MaxConnsPerTenant = 0, 2
	settings.MaxTotalPools = 1
	settings.HealthCheckPeriod, settings.PoolIdleTimeout = 0, 0
	manager := NewManager(settings, registry, log)
	defer manager.Close()
	ctx := context.Background()
	_, err = manager.GetPool(ctx, "disabled")
	require.ErrorIs(t, err, tenant.ErrTenantNotActive)
	require.Zero(t, manager.Stats().TotalPools)
	var wg sync.WaitGroup
	pools, errors := make(chan *ManagedPool, 16), make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); pool, err := manager.GetPool(ctx, "a"); pools <- pool; errors <- err }()
	}
	wg.Wait()
	close(pools)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var first *ManagedPool
	for pool := range pools {
		if first == nil {
			first = pool
		}
		require.Same(t, first, pool)
	}
	require.Equal(t, 1, manager.Stats().TotalPools)
	_, err = manager.GetPool(ctx, "b")
	require.ErrorIs(t, err, tenant.ErrMaxPoolLimit)
	first.AcquireRef()
	first.lastUsed.Store(time.Now().Add(-time.Hour).Unix())
	manager.config.PoolIdleTimeout = time.Minute // background eviction disabled for this deterministic fixture
	manager.evictIdlePools()
	require.Equal(t, 1, manager.Stats().TotalPools)
	require.Equal(t, 1, manager.Stats().Tenants[0].ActiveRefs)
	first.ReleaseRef()
	manager.evictIdlePools()
	require.Zero(t, manager.Stats().TotalPools)
	second, err := manager.GetPool(ctx, "b")
	require.NoError(t, err)
	require.NoError(t, second.Pool().Ping(ctx))
	manager.Close()
	require.Zero(t, manager.Stats().TotalPools)
	require.Error(t, second.Pool().Ping(ctx))
	_, err = manager.GetPool(ctx, "a")
	require.ErrorIs(t, err, context.Canceled)
}
