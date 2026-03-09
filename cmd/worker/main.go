// Package main is the entry point for the Metapus background worker.
// Multi-tenant architecture: processes jobs for all tenants.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

func main() {
	log, err := logger.New(logger.Config{
		Level:       getEnv("LOG_LEVEL", "info"),
		Development: getEnv("APP_ENV", "development") == "development",
	})
	if err != nil {
		fmt.Printf("failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Info("starting metapus multi-tenant worker")

	// Connect to meta-database
	metaPool, err := pgxpool.New(ctx, mustEnv("META_DATABASE_URL"))
	if err != nil {
		log.Fatalw("failed to connect to meta database", "error", err)
	}
	defer metaPool.Close()

	// Create tenant registry and manager
	registry := tenant.NewPostgresRegistry(metaPool)

	managerCfg := tenant.DefaultManagerConfig()
	managerCfg.DBUser = mustEnv("TENANT_DB_USER")
	managerCfg.DBPassword = mustEnv("TENANT_DB_PASSWORD")
	managerCfg.PoolIdleTimeout = 10 * time.Minute // Shorter for worker

	manager := tenant.NewManager(managerCfg, registry, log)
	defer manager.Close()

	// Start multi-tenant worker
	worker := NewMultiTenantWorker(manager, log)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		worker.Run(ctx)
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down worker...")
	cancel()

	wg.Wait()
	log.Info("worker stopped")
}

// MultiTenantWorker processes background jobs for all tenants.
type MultiTenantWorker struct {
	manager *tenant.Manager
	log     *logger.Logger
}

func NewMultiTenantWorker(manager *tenant.Manager, log *logger.Logger) *MultiTenantWorker {
	return &MultiTenantWorker{
		manager: manager,
		log:     log.WithComponent("worker"),
	}
}

// Run starts worker goroutines for all active tenants.
func (w *MultiTenantWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	var wg sync.WaitGroup
	tenantContexts := make(map[string]context.CancelFunc) // tenant_id(UUID) -> cancel
	var mu sync.Mutex

	// Initial start
	w.refreshTenants(ctx, &wg, tenantContexts, &mu)

	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			for _, cancel := range tenantContexts {
				cancel()
			}
			mu.Unlock()
			wg.Wait()
			return

		case <-ticker.C:
			w.refreshTenants(ctx, &wg, tenantContexts, &mu)
		}
	}
}

func (w *MultiTenantWorker) refreshTenants(ctx context.Context, wg *sync.WaitGroup, tenantContexts map[string]context.CancelFunc, mu *sync.Mutex) {
	tenants, err := w.manager.GetActiveTenants(ctx)
	if err != nil {
		w.log.Errorw("failed to get active tenants", "error", err)
		return
	}

	activeTenants := make(map[string]*tenant.Tenant, len(tenants))
	for _, t := range tenants {
		activeTenants[t.ID] = t
	}

	mu.Lock()
	defer mu.Unlock()

	for tenantID, cancel := range tenantContexts {
		if _, active := activeTenants[tenantID]; !active {
			cancel()
			delete(tenantContexts, tenantID)
			w.log.Infow("stopped worker for inactive tenant", "tenant_id", tenantID)
		}
	}

	for _, t := range tenants {
		if _, exists := tenantContexts[t.ID]; !exists {
			tenantCtx, tenantCancel := context.WithCancel(ctx)
			tenantContexts[t.ID] = tenantCancel

			wg.Add(1)
			go func(t *tenant.Tenant) {
				defer wg.Done()
				w.runTenantWorker(tenantCtx, t)
			}(t)

			w.log.Infow("started worker for tenant", "tenant_id", t.ID)
		}
	}
}

func (w *MultiTenantWorker) runTenantWorker(ctx context.Context, t *tenant.Tenant) {
	mp, err := w.manager.GetPool(ctx, t.ID)
	if err != nil {
		w.log.Errorw("failed to get pool for tenant", "tenant_id", t.ID, "error", err)
		return
	}

	txManager := postgres.NewTxManagerFromRawPool(mp.Pool())

	pollInterval := 500 * time.Millisecond
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Infow("stopping worker for tenant", "tenant_id", t.ID)
			return
		case <-ticker.C:
			w.processOutbox(ctx, mp.Pool(), txManager, t.ID)
		case <-cleanupTicker.C:
			w.cleanupSessions(ctx, mp.Pool(), t.ID)
			w.cleanupIdempotency(ctx, mp.Pool(), t.ID)
		}
	}
}

func (w *MultiTenantWorker) processOutbox(ctx context.Context, pool *pgxpool.Pool, txManager *postgres.TxManager, tenantID string) {
	rows, err := pool.Query(ctx, `
		SELECT id, aggregate_type, aggregate_id, event_type, payload, created_at
		FROM sys_outbox
		WHERE processed_at IS NULL
		ORDER BY created_at
		LIMIT 100
		FOR UPDATE SKIP LOCKED
	`)
	if err != nil {
		w.log.Debugw("outbox query failed (table may not exist)", "tenant_id", tenantID)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		// Process each message - implement actual processing logic here
	}

	if count > 0 {
		w.log.Debugw("processed outbox batch", "tenant_id", tenantID, "count", count)
	}
}

func (w *MultiTenantWorker) cleanupSessions(ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	result, err := pool.Exec(ctx, `
		DELETE FROM auth_refresh_tokens 
		WHERE expires_at < NOW() OR revoked = true
	`)
	if err != nil {
		return
	}

	if result.RowsAffected() > 0 {
		w.log.Infow("cleaned up expired sessions", "tenant_id", tenantID, "count", result.RowsAffected())
	}
}

func (w *MultiTenantWorker) cleanupIdempotency(ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	result, err := pool.Exec(ctx, `
		DELETE FROM sys_idempotency 
		WHERE created_at < NOW() - INTERVAL '24 hours'
	`)
	if err != nil {
		return
	}

	if result.RowsAffected() > 0 {
		w.log.Infow("cleaned up idempotency keys", "tenant_id", tenantID, "count", result.RowsAffected())
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func mustEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		fmt.Printf("required environment variable %s not set\n", key)
		os.Exit(1)
	}
	return value
}
