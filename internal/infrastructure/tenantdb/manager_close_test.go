package tenantdb

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	tenantcore "github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

func newTestManager(t *testing.T, registry tenantcore.Registry) *Manager {
	t.Helper()
	log, err := logger.New(logger.Config{Level: "error"})
	require.NoError(t, err)
	manager := NewManager(ManagerConfig{ConnectTimeout: time.Second}, registry, log)
	t.Cleanup(manager.Close)
	return manager
}

// storedPool puts an unconnected pool (MinConns 0 never dials) in the
// manager's map, as createPool would.
func storedPool(t *testing.T, m *Manager, tenantID string) *ManagedPool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/db?pool_min_conns=0")
	require.NoError(t, err)
	mp := &ManagedPool{pool: pool, tenant: &tenantcore.Tenant{ID: tenantID}}
	mp.Touch()
	m.pools.Store(tenantID, mp)
	m.poolCount.Add(1)
	return mp
}

func TestConcurrentClosesGiveThePoolSlotBackOnce(t *testing.T) {
	m := newTestManager(t, nil)
	mp := storedPool(t, m, "a")

	var wg sync.WaitGroup
	closed := make(chan bool, 8)
	for range 8 {
		wg.Go(func() { closed <- m.closeIdlePool("a", mp, "test") })
	}
	wg.Wait()
	close(closed)

	closes := 0
	for ok := range closed {
		if ok {
			closes++
		}
	}
	require.Equal(t, 1, closes)
	require.Zero(t, m.poolCount.Load())
	_, ok := m.pools.Load("a")
	require.False(t, ok)
}

func TestReferencedPoolIsNotClosedAndClosedPoolIsNotReferenced(t *testing.T) {
	m := newTestManager(t, nil)
	mp := storedPool(t, m, "a")

	held, release, err := m.Acquire(context.Background(), "a")
	require.NoError(t, err)
	require.Same(t, mp, held)
	require.False(t, m.closeIdlePool("a", mp, "test"))

	release()
	release()
	require.Zero(t, mp.refCount.Load())
	require.True(t, m.closeIdlePool("a", mp, "test"))
	require.False(t, mp.acquire())
}

// blockingRegistry holds the lookup until the test lets it go, then reports
// whether the context it was given had been cancelled.
type blockingRegistry struct {
	tenantcore.Registry
	started chan struct{}
	proceed chan struct{}
}

func (r blockingRegistry) GetByID(ctx context.Context, _ string) (*tenantcore.Tenant, error) {
	close(r.started)
	<-r.proceed
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, tenantcore.ErrTenantNotFound
}

func TestPoolCreationOutlivesTheFirstCallersRequest(t *testing.T) {
	registry := blockingRegistry{started: make(chan struct{}), proceed: make(chan struct{})}
	m := newTestManager(t, registry)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := m.GetPool(ctx, "a")
		result <- err
	}()
	<-registry.started
	cancel()
	close(registry.proceed)

	// The lookup ran on a context of its own: the registry answered, rather
	// than every waiter on this tenant getting the first caller's cancellation.
	require.ErrorIs(t, <-result, tenantcore.ErrTenantNotFound)
	require.Zero(t, m.poolCount.Load())
}
