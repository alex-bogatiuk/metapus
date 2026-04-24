// Package main is the entry point for the Metapus background worker.
// Multi-tenant architecture: processes jobs for all tenants.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/automation"
	"metapus/internal/core/automation/adapters"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	ws "metapus/internal/infrastructure/websocket"
	"metapus/pkg/logger"
)

// Version information — set via ldflags at build time.
var (
	Version   = "dev"
	BuildTime = "unknown"
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

	log.Infow("starting metapus multi-tenant worker",
		"version", Version,
		"build_time", BuildTime,
	)

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

	// Cloud mode: restrict worker to process only tenants in this version group.
	versionGroup := getEnv("VERSION_GROUP", "")
	if versionGroup != "" {
		managerCfg.VersionGroup = versionGroup
		log.Infow("cloud mode: version group filter enabled", "version_group", versionGroup)
	}

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
	// Use version-group-aware listing if configured (cloud mode).
	tenants, err := w.manager.ListByVersionGroup(ctx, w.manager.Config().VersionGroup)
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

	// Hold a reference so the eviction loop doesn't close our pool.
	// evictIdlePools() skips pools with refCount > 0.
	mp.AcquireRef()
	defer mp.ReleaseRef()

	txManager := postgres.NewTxManagerFromRawPool(mp.Pool())

	// Initialize automation engine ONCE per tenant worker lifecycle.
	// All repos are stateless (they extract pool from ctx), so reuse is safe.
	engine, err := w.buildAutomationEngine()
	if err != nil {
		w.log.Errorw("failed to initialize automation engine", "tenant_id", t.ID, "error", err)
		return
	}

	handler := &automationOutboxHandler{engine: engine, log: w.log}
	relay := postgres.NewOutboxRelay(mp.Pool(), 100, handler)

	pollInterval := 500 * time.Millisecond
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	// Enrich context with Pool and TxManager so that repos can access them.
	ctx = tenant.WithPool(ctx, mp.Pool())
	ctx = tenant.WithTxManager(ctx, txManager)

	// Start CRON scheduler for scheduled rules (runs in background goroutine).
	scheduler := automation.NewScheduler(engine, postgres.NewAutomationRuleRepo())
	go scheduler.Start(ctx) // Will stop when ctx is cancelled

	for {
		select {
		case <-ctx.Done():
			w.log.Infow("stopping worker for tenant", "tenant_id", t.ID)
			return
		case <-ticker.C:
			// Keep pool alive — prevent idle eviction.
			mp.Touch()

			processed, err := relay.ProcessBatch(ctx)
			if err != nil {
				w.log.Errorw("failed to process outbox batch", "tenant_id", t.ID, "error", err)
			} else if processed > 0 {
				w.log.Debugw("processed outbox batch", "tenant_id", t.ID, "count", processed)
			}
		case <-cleanupTicker.C:
			mp.Touch()
			w.cleanupSessions(ctx, mp.Pool(), t.ID)
			w.cleanupIdempotency(ctx, mp.Pool(), t.ID)
			w.cleanupAutomationHistory(ctx, mp.Pool(), t.ID)
			w.cleanupNotifications(ctx, mp.Pool(), t.ID)
			// Refresh scheduler jobs (picks up new/deactivated scheduled rules)
			scheduler.Refresh(ctx)
		}
	}
}

// buildAutomationEngine creates a reusable Engine with all adapters.
func (w *MultiTenantWorker) buildAutomationEngine() (*automation.Engine, error) {
	ruleRepo := postgres.NewAutomationRuleRepo()
	accountRepo := postgres.NewAutomationAccountRepo()
	channelRepo := postgres.NewAutomationChannelRepo()
	historyRepo := postgres.NewAutomationHistoryRepo()

	adapterMap := map[string]automation.Adapter{
		"webhook":  automation.NewWebhookAdapter(),
		"telegram": automation.NewTelegramAdapter(),
		"email":    automation.NewEmailAdapter(),
		adapters.InternalNotificationProvider: adapters.NewInternalNotificationAdapter(postgres.NewNotificationRepo(), ws.GlobalHub),
	}

	// OutboxPublisher is nil for now — chain reactions will be wired in a future iteration.
	return automation.NewEngine(ruleRepo, historyRepo, accountRepo, accountRepo, channelRepo, adapterMap, nil)
}

type automationOutboxHandler struct {
	engine *automation.Engine
	log    *logger.Logger
}

func (h *automationOutboxHandler) Handle(ctx context.Context, msg *postgres.OutboxMessage) error {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		h.log.Errorw("failed to unmarshal outbox payload", "error", err, "msg_id", msg.ID)
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	if msg.AggregateType == "automation_history" && msg.EventType == "replay" {
		historyIDStr, _ := payload["history_id"].(string)
		historyID, err := id.Parse(historyIDStr)
		if err != nil {
			h.log.Errorw("invalid history_id for replay", "error", err)
			return nil
		}
		if err := h.engine.DeliverReplay(ctx, historyID); err != nil {
			h.log.Errorw("failed to replay automation", "history_id", historyID, "error", err)
			return err
		}
		return nil
	}

	return h.engine.HandleEvent(ctx, msg.EventType, payload)
}

func (w *MultiTenantWorker) cleanupSessions(ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	result, err := pool.Exec(ctx, `
		DELETE FROM refresh_tokens 
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

func (w *MultiTenantWorker) cleanupAutomationHistory(ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	result, err := pool.Exec(ctx, `
		DELETE FROM sys_automation_history 
		WHERE created_at < NOW() - INTERVAL '30 days'
	`)
	if err != nil {
		w.log.Errorw("failed to cleanup automation history", "tenant_id", tenantID, "error", err)
		return
	}

	if result.RowsAffected() > 0 {
		w.log.Infow("cleaned up automation history", "tenant_id", tenantID, "count", result.RowsAffected())
	}
}

func (w *MultiTenantWorker) cleanupNotifications(ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	result, err := pool.Exec(ctx, `
		DELETE FROM sys_notifications 
		WHERE is_read = true AND created_at < NOW() - INTERVAL '30 days'
	`)
	if err != nil {
		w.log.Errorw("failed to cleanup notifications", "tenant_id", tenantID, "error", err)
		return
	}

	if result.RowsAffected() > 0 {
		w.log.Infow("cleaned up old read notifications", "tenant_id", tenantID, "count", result.RowsAffected())
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
