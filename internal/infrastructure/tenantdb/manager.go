package tenantdb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	tenantcore "metapus/internal/core/tenant"
	"metapus/pkg/logger"
)

// ManagerConfig configures MultiTenantManager behavior.
type ManagerConfig struct {
	// Database credentials for tenant databases
	DBUser     string
	DBPassword string

	// Pool settings (per tenant)
	MaxConnsPerTenant int32
	MinConnsPerTenant int32

	// Connection settings
	ConnectTimeout time.Duration

	// Lifecycle settings
	MaxTotalPools     int           // Max simultaneous pools (0 = unlimited)
	PoolIdleTimeout   time.Duration // Close pool after inactivity (0 = never)
	HealthCheckPeriod time.Duration // How often to check pool health

	// ConfigurePool injects infrastructure-specific connection setup. Product
	// entry points use it to install the PostgreSQL JSON v2 codecs without
	// coupling the tenant manager to an infrastructure package.
	ConfigurePool func(*pgxpool.Config)
}

// DefaultManagerConfig returns production-safe defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		MaxConnsPerTenant: 10,
		MinConnsPerTenant: 2,
		ConnectTimeout:    10 * time.Second,
		MaxTotalPools:     100,
		PoolIdleTimeout:   30 * time.Minute,
		HealthCheckPeriod: 1 * time.Minute,
	}
}

// ManagedPool wraps pgxpool.Pool with lifecycle tracking.
type ManagedPool struct {
	pool     *pgxpool.Pool
	tenant   *tenantcore.Tenant
	lastUsed atomic.Int64 // Unix timestamp
	refCount atomic.Int32 // Active requests using this pool
	// unhealthySince is set when health check fails (unix timestamp). 0 means healthy/unknown.
	unhealthySince atomic.Int64
}

// Touch updates last used timestamp.
func (mp *ManagedPool) Touch() {
	mp.lastUsed.Store(time.Now().Unix())
}

// Pool returns underlying pgxpool.Pool.
func (mp *ManagedPool) Pool() *pgxpool.Pool {
	return mp.pool
}

// Tenant returns tenant info.
func (mp *ManagedPool) Tenant() *tenantcore.Tenant {
	return mp.tenant
}

// AcquireRef increments reference count (for tracking active requests).
func (mp *ManagedPool) AcquireRef() {
	mp.refCount.Add(1)
}

// ReleaseRef decrements reference count.
func (mp *ManagedPool) ReleaseRef() {
	mp.refCount.Add(-1)
}

// Manager manages database connections for multiple tenants.
// Thread-safe for concurrent access.
type Manager struct {
	config   ManagerConfig
	registry tenantcore.Registry

	pools     sync.Map // map[tenantID]*ManagedPool
	poolCount atomic.Int32

	sf singleflight.Group

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	log    *logger.Logger
	// Shutdown waits for pool creation and rejects subsequent requests.
	lifecycle sync.RWMutex
}

// NewManager creates a new multi-tenant connection manager.
func NewManager(cfg ManagerConfig, registry tenantcore.Registry, log *logger.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		config:   cfg,
		registry: registry,
		ctx:      ctx,
		cancel:   cancel,
		log:      log.WithComponent("tenant-manager"),
	}

	// Start background workers
	if cfg.PoolIdleTimeout > 0 {
		m.wg.Add(1)
		go m.evictionLoop()
	}

	if cfg.HealthCheckPeriod > 0 {
		m.wg.Add(1)
		go m.healthCheckLoop()
	}

	m.log.Info("multi-tenant manager started",
		"max_pools", cfg.MaxTotalPools,
		"idle_timeout", cfg.PoolIdleTimeout,
		"health_check_period", cfg.HealthCheckPeriod,
	)

	return m
}

// GetPool returns database pool for tenant, creating if needed.
func (m *Manager) GetPool(ctx context.Context, tenantID string) (*ManagedPool, error) {
	m.lifecycle.RLock()
	defer m.lifecycle.RUnlock()
	if err := m.ctx.Err(); err != nil {
		return nil, err
	}
	// Fast path: pool exists
	if val, ok := m.pools.Load(tenantID); ok {
		mp := val.(*ManagedPool)
		mp.Touch()
		return mp, nil
	}

	// Slow path: create new pool
	return m.createPool(ctx, tenantID)
}

// createPool creates a new connection pool for tenant.
func (m *Manager) createPool(ctx context.Context, tenantID string) (*ManagedPool, error) {
	v, err, _ := m.sf.Do(tenantID, func() (any, error) {
		// Reserve pool slot (pre-increment to prevent TOCTOU race).
		// If we fail to create the pool or another goroutine wins LoadOrStore,
		// we rollback by decrementing.
		if m.config.MaxTotalPools > 0 {
			newCount := m.poolCount.Add(1)
			if int(newCount) > m.config.MaxTotalPools {
				m.poolCount.Add(-1)
				return nil, fmt.Errorf("%w (%d)", tenantcore.ErrMaxPoolLimit, m.config.MaxTotalPools)
			}
		}

		// rollbackSlot decrements poolCount if we reserved a slot but failed to use it.
		rollbackSlot := func() {
			if m.config.MaxTotalPools > 0 {
				m.poolCount.Add(-1)
			}
		}

		// Get tenant info from registry
		tenant, err := m.registry.GetByID(ctx, tenantID)
		if err != nil {
			rollbackSlot()
			if errors.Is(err, tenantcore.ErrTenantNotFound) {
				return nil, err
			}
			return nil, fmt.Errorf("tenant lookup failed: %w", err)
		}

		if !tenant.CanCreatePool() {
			rollbackSlot()
			return nil, fmt.Errorf("%w: status=%s", tenantcore.ErrTenantNotActive, tenant.Status)
		}

		// Build DSN and create pool config
		dsn := DSN(tenant, m.config.DBUser, m.config.DBPassword)

		poolCfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			rollbackSlot()
			return nil, fmt.Errorf("parse dsn for tenant %s: %w", tenantID, err)
		}

		poolCfg.MaxConns = m.config.MaxConnsPerTenant
		poolCfg.MinConns = m.config.MinConnsPerTenant
		// Zero disables this manager's loop, not pgxpool's internal maintenance.
		// Keep pgxpool's positive default to avoid a zero-duration ticker panic.
		if m.config.HealthCheckPeriod > 0 {
			poolCfg.HealthCheckPeriod = m.config.HealthCheckPeriod
		}
		poolCfg.ConnConfig.ConnectTimeout = m.config.ConnectTimeout
		if m.config.ConfigurePool != nil {
			m.config.ConfigurePool(poolCfg)
		}

		// Create pool with timeout
		createCtx, cancel := context.WithTimeout(ctx, m.config.ConnectTimeout)
		defer cancel()

		pool, err := pgxpool.NewWithConfig(createCtx, poolCfg)
		if err != nil {
			rollbackSlot()
			return nil, fmt.Errorf("create pool for tenant %s: %w", tenantID, err)
		}

		// Verify connection
		if err := pool.Ping(createCtx); err != nil {
			pool.Close()
			rollbackSlot()
			return nil, fmt.Errorf("ping tenant %s: %w", tenantID, err)
		}

		mp := &ManagedPool{
			pool:   pool,
			tenant: tenant,
		}
		mp.Touch()

		// Store (handle race condition — another goroutine might have created it
		// just before we entered singleflight, or if singleflight is bypassed)
		actual, loaded := m.pools.LoadOrStore(tenantID, mp)
		if loaded {
			// Another goroutine created pool first, close ours and return theirs
			pool.Close()
			rollbackSlot() // release our reserved slot
			return actual.(*ManagedPool), nil
		}

		m.log.Info("created pool for tenant",
			"tenant_id", tenantID,
			"db_name", tenant.DBName,
			"total_pools", m.poolCount.Load(),
		)

		return mp, nil
	})

	if err != nil {
		return nil, err
	}
	return v.(*ManagedPool), nil
}

// evictionLoop closes idle pools periodically.
func (m *Manager) evictionLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.PoolIdleTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.evictIdlePools()
		}
	}
}

// evictIdlePools closes pools that haven't been used recently.
func (m *Manager) evictIdlePools() {
	threshold := time.Now().Add(-m.config.PoolIdleTimeout).Unix()

	m.pools.Range(func(key, value any) bool {
		tenantID := key.(string)
		mp := value.(*ManagedPool)

		// Don't evict if actively in use
		if mp.refCount.Load() > 0 {
			return true
		}

		// If pool was marked unhealthy and is not in use, close it ASAP.
		if mp.unhealthySince.Load() > 0 {
			m.closePool(tenantID, mp, "unhealthy pool (no active refs)")
			return true
		}

		if mp.lastUsed.Load() < threshold {
			m.closePool(tenantID, mp, "idle timeout")
		}

		return true
	})
}

// healthCheckLoop monitors pool health.
func (m *Manager) healthCheckLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.HealthCheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkPoolsHealth()
		}
	}
}

// checkPoolsHealth pings all pools and closes unhealthy ones.
func (m *Manager) checkPoolsHealth() {
	m.pools.Range(func(key, value any) bool {
		tenantID := key.(string)
		mp := value.(*ManagedPool)

		// Create timeout per pool, not globally, to prevent cascading failures
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()

		if err := mp.pool.Ping(ctx); err != nil {
			if mp.unhealthySince.Load() == 0 {
				mp.unhealthySince.Store(time.Now().Unix())
			}
			m.log.Warn("pool health check failed",
				"tenant_id", tenantID,
				"error", err,
			)
			// Never close pools that are currently used by active requests.
			// Close as soon as refCount reaches zero (see eviction loop).
			if mp.refCount.Load() == 0 {
				m.closePool(tenantID, mp, "health check failed")
			}
			return true
		}

		// Healthy again.
		if mp.unhealthySince.Load() != 0 {
			mp.unhealthySince.Store(0)
		}
		return true
	})
}

// closePool safely closes a managed pool.
func (m *Manager) closePool(tenantID string, mp *ManagedPool, reason string) {
	m.pools.Delete(tenantID)
	mp.pool.Close()
	m.poolCount.Add(-1)

	m.log.Info("closed pool",
		"tenant_id", tenantID,
		"reason", reason,
		"total_pools", m.poolCount.Load(),
	)
}

// Close shuts down manager and all pools gracefully.
func (m *Manager) Close() {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	if m.ctx.Err() != nil {
		return
	}
	m.log.Info("shutting down multi-tenant manager...")

	// Stop background workers
	m.cancel()
	m.wg.Wait()

	// Close all pools and remove from map
	var poolsClosed int
	m.pools.Range(func(key, value any) bool {
		mp := value.(*ManagedPool)
		mp.pool.Close()
		m.pools.Delete(key)
		poolsClosed++
		return true
	})
	m.poolCount.Store(0)

	m.log.Info("multi-tenant manager closed", "pools_closed", poolsClosed)
}

// Stats returns current manager statistics.
func (m *Manager) Stats() ManagerStats {
	var stats ManagerStats
	stats.TotalPools = int(m.poolCount.Load())

	m.pools.Range(func(key, value any) bool {
		mp := value.(*ManagedPool)
		poolStats := mp.pool.Stat()

		stats.TotalConns += int(poolStats.TotalConns())
		stats.IdleConns += int(poolStats.IdleConns())
		stats.AcquiredConns += int(poolStats.AcquiredConns())

		stats.Tenants = append(stats.Tenants, TenantPoolStats{
			TenantID:      key.(string),
			DBName:        mp.tenant.DBName,
			TotalConns:    int(poolStats.TotalConns()),
			IdleConns:     int(poolStats.IdleConns()),
			AcquiredConns: int(poolStats.AcquiredConns()),
			ActiveRefs:    int(mp.refCount.Load()),
			LastUsed:      time.Unix(mp.lastUsed.Load(), 0),
		})
		return true
	})

	return stats
}

// ManagerStats contains manager runtime statistics.
type ManagerStats struct {
	TotalPools    int
	TotalConns    int
	IdleConns     int
	AcquiredConns int
	Tenants       []TenantPoolStats
}

// TenantPoolStats contains per-tenant pool statistics.
type TenantPoolStats struct {
	TenantID      string
	DBName        string
	TotalConns    int
	IdleConns     int
	AcquiredConns int
	ActiveRefs    int
	LastUsed      time.Time
}

// GetActiveTenants returns list of all active tenants from registry.
func (m *Manager) GetActiveTenants(ctx context.Context) ([]*tenantcore.Tenant, error) {
	return m.registry.ListActive(ctx)
}

// GetRegistry returns the tenant registry.
func (m *Manager) GetRegistry() tenantcore.Registry {
	return m.registry
}

// PrewarmPools creates pools for all active tenants.
// Useful for reducing latency on first requests.
func (m *Manager) PrewarmPools(ctx context.Context) error {
	tenants, err := m.registry.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active tenants: %w", err)
	}

	m.log.Info("prewarming pools", "tenant_count", len(tenants))

	var wg sync.WaitGroup
	errCh := make(chan error, len(tenants))

	for _, t := range tenants {
		wg.Add(1)
		go func(tenant *tenantcore.Tenant) {
			defer wg.Done()

			if _, err := m.GetPool(ctx, tenant.ID); err != nil {
				errCh <- fmt.Errorf("prewarm %s: %w", tenant.ID, err)
			}
		}(t)
	}

	wg.Wait()
	close(errCh)

	// Collect errors
	errs := make([]error, 0, len(errCh))
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to prewarm %d tenant pools: %w", len(errs), errors.Join(errs...))
	}

	m.log.Info("all pools prewarmed successfully")
	return nil
}
