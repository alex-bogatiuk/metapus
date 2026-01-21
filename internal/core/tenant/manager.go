package tenant

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
	tenant   *Tenant
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
func (mp *ManagedPool) Tenant() *Tenant {
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
	registry Registry

	pools     sync.Map // map[tenantID]*ManagedPool
	poolCount atomic.Int32

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	log    *logger.Logger
}

// NewManager creates a new multi-tenant connection manager.
func NewManager(cfg ManagerConfig, registry Registry, log *logger.Logger) *Manager {
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
	// Check limits
	if m.config.MaxTotalPools > 0 && int(m.poolCount.Load()) >= m.config.MaxTotalPools {
		return nil, fmt.Errorf("%w (%d)", ErrMaxPoolLimit, m.config.MaxTotalPools)
	}

	// Get tenant info from registry
	tenant, err := m.registry.GetByID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, ErrTenantNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("tenant lookup failed: %w", err)
	}

	if !tenant.IsActive() {
		return nil, fmt.Errorf("%w: status=%s", ErrTenantNotActive, tenant.Status)
	}

	// Build DSN and create pool config
	dsn := tenant.DSN(m.config.DBUser, m.config.DBPassword)

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn for tenant %s: %w", tenantID, err)
	}

	poolCfg.MaxConns = m.config.MaxConnsPerTenant
	poolCfg.MinConns = m.config.MinConnsPerTenant
	poolCfg.HealthCheckPeriod = m.config.HealthCheckPeriod
	poolCfg.ConnConfig.ConnectTimeout = m.config.ConnectTimeout

	// Create pool with timeout
	createCtx, cancel := context.WithTimeout(ctx, m.config.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(createCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool for tenant %s: %w", tenantID, err)
	}

	// Verify connection
	if err := pool.Ping(createCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping tenant %s: %w", tenantID, err)
	}

	mp := &ManagedPool{
		pool:   pool,
		tenant: tenant,
	}
	mp.Touch()

	// Store (handle race condition - another goroutine might have created it)
	actual, loaded := m.pools.LoadOrStore(tenantID, mp)
	if loaded {
		// Another goroutine created pool first, close ours and return theirs
		pool.Close()
		return actual.(*ManagedPool), nil
	}

	m.poolCount.Add(1)
	m.log.Info("created pool for tenant",
		"tenant_id", tenantID,
		"db_name", tenant.DBName,
		"total_pools", m.poolCount.Load(),
	)

	return mp, nil
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
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()

	m.pools.Range(func(key, value any) bool {
		tenantID := key.(string)
		mp := value.(*ManagedPool)

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
	m.log.Info("shutting down multi-tenant manager...")

	// Stop background workers
	m.cancel()
	m.wg.Wait()

	// Close all pools
	var poolsClosed int
	m.pools.Range(func(key, value any) bool {
		mp := value.(*ManagedPool)
		mp.pool.Close()
		poolsClosed++
		return true
	})

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
func (m *Manager) GetActiveTenants(ctx context.Context) ([]*Tenant, error) {
	return m.registry.ListActive(ctx)
}

// GetRegistry returns the tenant registry.
func (m *Manager) GetRegistry() Registry {
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
		go func(tenant *Tenant) {
			defer wg.Done()

			if _, err := m.GetPool(ctx, tenant.ID); err != nil {
				errCh <- fmt.Errorf("prewarm %s: %w", tenant.ID, err)
			}
		}(t)
	}

	wg.Wait()
	close(errCh)

	// Collect errors
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		m.log.Warn("some pools failed to prewarm", "error_count", len(errors))
		// Return first error
		return errors[0]
	}

	m.log.Info("all pools prewarmed successfully")
	return nil
}
