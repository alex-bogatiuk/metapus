// Package main is the entry point for the Metapus background worker.
// Multi-tenant architecture: processes jobs for all tenants.
package main

import (
	"context"
	"fmt"
	"metapus/internal/infrastructure/crypto/webhook"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"metapus/internal/application/automation"
	"metapus/internal/application/readaccess"
	"metapus/internal/content"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain/catalogs/crypto_provider_account"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/automation/adapters"
	automationdelivery "metapus/internal/infrastructure/automation/delivery"
	automationreport "metapus/internal/infrastructure/automation/reporting"
	chainbootstrap "metapus/internal/infrastructure/blockchain/bootstrap"
	"metapus/internal/infrastructure/crypto/resources/itrx"
	"metapus/internal/infrastructure/crypto/webhooksecret"
	"metapus/internal/infrastructure/crypto_worker"
	"metapus/internal/infrastructure/rate_feed"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/auth_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/infrastructure/storage/postgres/security_repo"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/internal/infrastructure/vaultclient"
	ws "metapus/internal/infrastructure/websocket"
	"metapus/internal/platform/secretfile"
	platformtelemetry "metapus/internal/platform/telemetry"
	"metapus/pkg/logger"
)

// Version information — set via ldflags at build time.
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if err := validateProductionVaultEnvironment(os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
	shutdownMetrics, err := platformtelemetry.ConfigureMetrics(ctx, "metapus-worker", Version, os.Getenv)
	if err != nil {
		log.Fatalw("failed to initialize OTLP metrics", "error", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if shutdownErr := shutdownMetrics(shutdownCtx); shutdownErr != nil {
			log.Errorw("failed to flush OTLP metrics", "error", shutdownErr)
		}
	}()
	shutdownTracing, err := platformtelemetry.ConfigureTracing(ctx, "metapus-worker", Version, os.Getenv)
	if err != nil {
		log.Fatalw("failed to initialize OTLP tracing", "error", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if shutdownErr := shutdownTracing(shutdownCtx); shutdownErr != nil {
			log.Errorw("failed to flush OTLP traces", "error", shutdownErr)
		}
	}()

	log.Infow("starting metapus multi-tenant worker",
		"version", Version,
		"build_time", BuildTime,
	)

	// Connect to meta-database
	metaPool, err := postgres.OpenPool(ctx, mustEnv("META_DATABASE_URL"))
	if err != nil {
		log.Fatalw("failed to connect to meta database", "error", err)
	}
	defer metaPool.Close()

	// Create tenant registry and manager
	registry := tenantdb.NewPostgresRegistry(metaPool)

	managerCfg := tenantdb.DefaultManagerConfig()
	managerCfg.DBUser = mustEnv("TENANT_DB_USER")
	managerCfg.DBPassword = mustEnv("TENANT_DB_PASSWORD")
	managerCfg.PoolIdleTimeout = 10 * time.Minute // Shorter for worker
	managerCfg.ConfigurePool = postgres.ConfigurePool

	manager := tenantdb.NewManager(managerCfg, registry, log)
	defer manager.Close()

	// Webhook signing secrets are resolved from Vault at each delivery attempt.
	var webhookSecrets crypto.WebhookSecretStore
	if vaultAddr := strings.TrimSpace(getEnv("VAULT_ADDR", "")); vaultAddr != "" {
		client, vaultErr := vaultclient.New(vaultclient.Config{Addr: vaultAddr, Token: strings.TrimSpace(getEnv("VAULT_TOKEN", ""))})
		if vaultErr != nil {
			log.Fatalw("failed to create webhook Vault client", "error", vaultErr)
		}
		webhookSecrets, vaultErr = webhooksecret.NewVaultStore(client, getEnv("WEBHOOK_SECRET_KV_MOUNT", "secret"))
		if vaultErr != nil {
			log.Fatalw("failed to create webhook secret store", "error", vaultErr)
		}
	}

	// Start multi-tenant worker
	worker := NewMultiTenantWorker(manager, log, webhookSecrets, crypto_worker.NewInvoiceCheckoutHandler(postgres.NewCheckoutReferenceRepo(metaPool)))

	var wg sync.WaitGroup
	wg.Go(func() {
		worker.Run(ctx)
	})

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down worker...")
	cancel()

	wg.Wait()
	log.Info("worker stopped")
}

func validateProductionVaultEnvironment(getenv func(string) string) error {
	if !strings.EqualFold(strings.TrimSpace(getenv("APP_ENV")), "production") {
		return nil
	}
	if strings.TrimSpace(getenv("VAULT_TOKEN")) != "" {
		return fmt.Errorf("VAULT_TOKEN is forbidden in production; use tokenless Vault Proxy mode")
	}
	if strings.TrimSpace(getenv("VAULT_TOKEN_FILE")) != "" {
		return fmt.Errorf("VAULT_TOKEN_FILE is forbidden in production; use tokenless Vault Proxy mode")
	}
	if strings.TrimSpace(getenv("CRYPTO_SIGNING_AUTH_PRIVATE_KEY")) != "" {
		return fmt.Errorf("CRYPTO_SIGNING_AUTH_PRIVATE_KEY is forbidden in production; use Vault Transit")
	}
	if strings.TrimSpace(getenv("CRYPTO_SIGNING_AUTH_PRIVATE_KEY_FILE")) != "" {
		return fmt.Errorf("CRYPTO_SIGNING_AUTH_PRIVATE_KEY_FILE is forbidden in production; use Vault Transit")
	}
	mode := strings.ToLower(strings.TrimSpace(getenv("CRYPTO_AUTH_SIGNER")))
	if mode != "" && mode != "transit" {
		return fmt.Errorf("production CRYPTO_AUTH_SIGNER must be transit or empty")
	}
	if mode == "transit" && strings.TrimSpace(getenv("VAULT_ADDR")) == "" {
		return fmt.Errorf("VAULT_ADDR is required for production Transit signing")
	}
	if strings.TrimSpace(getenv("VAULT_ADDR")) == "" {
		return fmt.Errorf("VAULT_ADDR is required for production webhook secrets")
	}
	if strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")) == "" &&
		strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		return fmt.Errorf("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT or OTEL_EXPORTER_OTLP_ENDPOINT is required in production")
	}
	return nil
}

// MultiTenantWorker processes background jobs for all tenants.
type MultiTenantWorker struct {
	manager            *tenantdb.Manager
	log                *logger.Logger
	webhookSecrets     crypto.WebhookSecretStore
	checkoutReferences *crypto_worker.InvoiceCheckoutHandler
	ready              metric.Int64Gauge
	restarts           metric.Int64Counter
}

func NewMultiTenantWorker(manager *tenantdb.Manager, log *logger.Logger, webhookSecrets crypto.WebhookSecretStore, checkoutReferences *crypto_worker.InvoiceCheckoutHandler) *MultiTenantWorker {
	ready, _ := otel.Meter("metapus/worker").Int64Gauge("worker.component.ready")
	restarts, _ := otel.Meter("metapus/worker").Int64Counter("worker.component.restarts")
	return &MultiTenantWorker{
		manager: manager,
		ready:   ready, restarts: restarts,
		log:                log.WithComponent("worker"),
		webhookSecrets:     webhookSecrets,
		checkoutReferences: checkoutReferences,
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
				w.supervise(tenantCtx, t.ID, "tenant", func(ctx context.Context) error { return w.runTenantWorker(ctx, t) })
			}(t)

			w.log.Infow("started worker for tenant", "tenant_id", t.ID)
		}
	}
}

func (w *MultiTenantWorker) runTenantWorker(ctx context.Context, t *tenant.Tenant) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	mp, err := w.manager.GetPool(ctx, t.ID)
	if err != nil {
		w.log.Errorw("failed to get pool for tenant", "tenant_id", t.ID, "error", err)
		return err
	}

	// Hold a reference so the eviction loop doesn't close our pool.
	// evictIdlePools() skips pools with refCount > 0.
	mp.AcquireRef()
	defer mp.ReleaseRef()
	if err := validateWorkerSchema(ctx, mp.Pool()); err != nil {
		return err
	}

	txManager := postgres.NewTxManagerFromRawPool(mp.Pool())

	// ── Worker Job Recorder (observability) ────────────────────────────
	// Writes to sys_worker_jobs for the /admin/worker-jobs UI.
	// Best-effort: never fails the worker on repo errors.
	jobRepo := postgres.NewWorkerJobRepo(mp.Pool())
	recorder := workerjob.NewRecorder(jobRepo, w.log)

	// Initialize automation engine ONCE per tenant worker lifecycle.
	// All repos are stateless (they extract pool from ctx), so reuse is safe.
	policies, err := security.NewPolicyEngine()
	if err != nil {
		w.log.Errorw("failed to initialize read policies", "error", err)
		return err
	}
	defer policies.Stop()
	engine, err := w.buildAutomationEngine(policies)
	if err != nil {
		w.log.Errorw("failed to initialize automation engine", "tenant_id", t.ID, "error", err)
		return err
	}

	webhookRepo := crypto_repo.NewWebhookDeliveryRepo()
	merchantWebhookRepo := crypto_repo.NewMerchantWebhookRepo()
	handler := &automationOutboxHandler{
		engine:             engine,
		log:                w.log,
		checkoutReferences: w.checkoutReferences,
		merchantWebhooks: crypto_worker.NewMerchantWebhookHandler(
			webhook.NewDispatcher(), webhookRepo, merchantWebhookRepo, w.webhookSecrets,
		),
	}

	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	// Enrich context with Pool and TxManager so that repos can access them.
	ctx = tenantdb.WithPool(ctx, mp.Pool())
	ctx = tenant.WithTxManager(ctx, txManager)
	ctx = tenant.WithTenant(ctx, t)

	// subsWg tracks goroutines (scheduler, crypto processor) that use the
	// tenant pool. We must wait for them to exit BEFORE defer mp.ReleaseRef()
	// fires — otherwise the eviction loop can close the pool under them.
	// The goroutines all receive the same ctx and will stop on ctx.Done().
	var subsWg sync.WaitGroup
	defer func() { cancel(); subsWg.Wait() }()

	// Start CRON scheduler for scheduled rules (runs in background goroutine).
	scheduler := automation.NewScheduler(engine, postgres.NewAutomationRuleRepo(), postgres.NewAutomationRunRepo())
	subsWg.Go(func() {
		scheduler.Start(ctx) // blocks until ctx is cancelled
	})

	// ── Crypto Processing ──────────────────────────────────────────────
	// Start CryptoProcessor only by explicit feature flag and validated chain config.
	cryptoEnabled, cryptoEnabledErr := strconv.ParseBool(getEnv("CRYPTO_PROCESSING_ENABLED", "false"))
	if cryptoEnabledErr != nil {
		return fmt.Errorf("invalid CRYPTO_PROCESSING_ENABLED: %w", cryptoEnabledErr)
	}
	if cryptoEnabled {
		subsWg.Go(func() {
			w.supervise(ctx, t.ID, "crypto", func(runCtx context.Context) error {
				driverBuilder, err := chainbootstrap.NewBuilder(getEnv("CRYPTO_CHAIN_CONFIG_FILE", ""), getEnv("APP_ENV", "development"))
				if err != nil {
					return err
				}
				cp := crypto_worker.NewCryptoProcessor(crypto_worker.CryptoProcessorConfig{
					Ready:                          func(ready bool) { w.setReady(ctx, t.ID, "crypto", ready) },
					AppEnv:                         getEnv("APP_ENV", "development"),
					DriverRegistryBuilder:          driverBuilder,
					VaultAddr:                      getEnv("VAULT_ADDR", ""),
					VaultToken:                     getEnv("VAULT_TOKEN", ""),
					AuthorizationSigner:            getEnv("CRYPTO_AUTH_SIGNER", ""),
					TransitMount:                   getEnv("CRYPTO_TRANSIT_MOUNT", "transit"),
					LocalAuthorizationKID:          getEnv("CRYPTO_LOCAL_AUTH_KID", ""),
					SigningAuthorizationPrivateKey: getEnv("CRYPTO_SIGNING_AUTH_PRIVATE_KEY", ""),
					ApprovalPolicyPublicKey:        getEnv("CRYPTO_APPROVAL_POLICY_PUBLIC_KEY", ""),
					IntentSigningPublicKey:         getEnv("CRYPTO_INTENT_SIGNING_PUBLIC_KEY", ""),
					RiskAssessmentSigningPublicKey: getEnv("CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY", ""),
					PolicyDecisionSigningPublicKey: getEnv("CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY", ""),
					AdminApprovalSigningPublicKey:  getEnv("CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY", ""),
					RiskApprovalSigningPublicKey:   getEnv("CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY", ""),
					ApprovalPolicyBundleJWS:        getEnv("CRYPTO_APPROVAL_POLICY_BUNDLE_JWS", ""),
					ApprovalPolicyBundlesJWS:       getEnv("CRYPTO_APPROVAL_POLICY_BUNDLES_JWS", ""),
					TenantID:                       t.ID,
					JobRecorder:                    recorder,
					AccountProviderFactory:         buildAccountProviderFactory(getEnv("APP_ENV", "development")),
					ResourceLeaseTargetEnergy:      getEnvInt64("CRYPTO_RESOURCE_LEASE_TARGET_ENERGY", 65000),
					ResourceSafetyMarginPct:        getEnvInt64("CRYPTO_RESOURCE_SAFETY_MARGIN_PCT", 10),
					ResourceReservationCutoff:      time.Duration(getEnvInt64("CRYPTO_RESOURCE_RESERVATION_CUTOFF_SECONDS", 120)) * time.Second,
				}, w.log)
				return cp.Start(runCtx)
			})
		})
	}

	// ── Rate Feed (Exchange Rates) ─────────────────────────────────────
	// Periodically fetches crypto exchange rates from CoinGecko.
	// Loads currency→source mappings from reg_rate_source_mappings.
	{
		rfWorker := rate_feed.NewWorker(register_repo.NewExchangeRateRepo(), rate_feed.NewCoinGeckoFetcher(), recorder, w.log)
		subsWg.Go(func() { rfWorker.Start(ctx) })
	}

	launchRelay := func(name string, include, exclude []string) {
		relay := postgres.NewOutboxRelay(mp.Pool(), 1, handler).FilterAggregates(include, exclude)
		subsWg.Go(func() { w.runOutboxLoop(ctx, recorder, name, relay) })
	}
	launchRelay("outbox.checkout", []string{crypto_worker.InvoiceCheckoutAggregateType}, nil)
	launchRelay("outbox.webhook_jobs", []string{crypto.MerchantWebhookAggregateType}, nil)
	for range 2 {
		launchRelay("outbox.automation", nil, []string{crypto_worker.InvoiceCheckoutAggregateType, crypto.MerchantWebhookAggregateType})
	}
	delivery := crypto_worker.NewWebhookDeliveryWorker(merchantWebhookRepo, handler.merchantWebhooks, recorder)
	subsWg.Go(func() { delivery.Start(ctx) })
	recoveryRelay := postgres.NewOutboxRelay(mp.Pool(), 1, handler)
	subsWg.Go(func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for ctx.Err() == nil {
			recorder.RecordIfWork(ctx, "outbox.recover_stuck", "outbox", func(ctx context.Context) (int, error) {
				n, err := recoveryRelay.RecoverStuck(ctx, postgres.DefaultStuckTimeout())
				return int(n), err
			})
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	w.setReady(ctx, t.ID, "tenant", true)
	defer w.setReady(context.WithoutCancel(ctx), t.ID, "tenant", false)

	for {
		select {
		case <-ctx.Done():
			w.log.Infow("stopping worker for tenant", "tenant_id", t.ID)
			return ctx.Err()
		case <-cleanupTicker.C:
			mp.Touch()
			recorder.Record(ctx, "cleanup.sessions", "cleanup", func(ctx context.Context) (int, error) {
				return w.cleanupSessions(ctx, mp.Pool(), t.ID)
			})
			recorder.Record(ctx, "cleanup.idempotency", "cleanup", func(ctx context.Context) (int, error) {
				return w.cleanupIdempotency(ctx, mp.Pool(), t.ID)
			})
			recorder.Record(ctx, "cleanup.automation_history", "cleanup", func(ctx context.Context) (int, error) {
				return w.cleanupAutomationHistory(ctx, mp.Pool(), t.ID)
			})
			recorder.Record(ctx, "cleanup.automation_files", "cleanup", func(ctx context.Context) (int, error) {
				return w.cleanupAutomationFiles(ctx, mp.Pool(), t.ID)
			})
			recorder.Record(ctx, "cleanup.notifications", "cleanup", func(ctx context.Context) (int, error) {
				return w.cleanupNotifications(ctx, mp.Pool(), t.ID)
			})
			recorder.Record(ctx, "cleanup.worker_jobs", "cleanup", func(ctx context.Context) (int, error) {
				n, err := jobRepo.CleanupOld(ctx, 7*24*time.Hour)
				return int(n), err
			})
			if w.webhookSecrets != nil {
				recorder.RecordIfWork(ctx, "cleanup.webhook_secrets", "cleanup", func(ctx context.Context) (int, error) {
					return handler.merchantWebhooks.CleanupRetiredSecrets(ctx, 100)
				})
			}
			// Refresh scheduler jobs (picks up new/deactivated scheduled rules)
			scheduler.Refresh(ctx)
		}
	}
}

// buildAutomationEngine creates a reusable Engine with all adapters.
func (w *MultiTenantWorker) buildAutomationEngine(policies *security.PolicyEngine) (*automation.Engine, error) {
	ruleRepo := postgres.NewAutomationRuleRepo()
	accountRepo := postgres.NewAutomationAccountRepo()
	channelRepo := postgres.NewAutomationChannelRepo()
	historyRepo := postgres.NewAutomationHistoryRepo()

	adapterMap := map[string]automation.Adapter{
		"webhook":                             automationdelivery.NewWebhookAdapter(),
		"telegram":                            automationdelivery.NewTelegramAdapter(),
		"email":                               automationdelivery.NewEmailAdapter(),
		adapters.InternalNotificationProvider: adapters.NewInternalNotificationAdapter(postgres.NewNotificationRepo(), ws.GlobalHub),
	}

	// Resolve user/role subscribers to user IDs for internal notifications.
	roleRepo := auth_repo.NewRoleRepo()
	roleAdapter := adapters.NewAuthRoleAdapter(roleRepo)
	userResolver := automation.NewRoleUserResolver(roleAdapter)

	engine, err := automation.NewEngine(ruleRepo, historyRepo, accountRepo, accountRepo, channelRepo, adapterMap, userResolver)
	if err != nil {
		return nil, err
	}

	// ── Report Generator ────────────────────────────────────────────────
	// Build Compiler from the same datasets as the HTTP layer.
	datasets := content.AllDatasets()
	reportRegistry := content.BuildReportRegistry()
	comp := compiler.NewCompiler(reportRegistry, datasets, postgres.NewAuthorizedReadService(reportRegistry, policies), content.DatasetExecutors())

	fileRepo := postgres.NewAutomationFileRepo()
	settingsRepo := postgres.NewSettingsRepo()
	reportGen := automationreport.NewReportGenerator(comp, nil, fileRepo, &settingsLoaderAdapter{repo: settingsRepo}, readaccess.NewPrincipalLoader(auth_repo.NewUserRepo(), security_repo.NewProfileRepo(), crypto_repo.NewMerchantDimensionResolver()))

	emailRenderer, err := automationreport.NewEmailTemplateRenderer()
	if err != nil {
		return nil, fmt.Errorf("init email renderer: %w", err)
	}

	engine.SetReportGenerator(reportGen, emailRenderer)

	return engine, nil
}

// settingsLoaderAdapter bridges postgres.SettingsRepo (Get) → automation.SettingsLoader (GetSettings).
type settingsLoaderAdapter struct {
	repo *postgres.SettingsRepo
}

func (a *settingsLoaderAdapter) GetSettings(ctx context.Context) (*settings.Settings, error) {
	return a.repo.Get(ctx)
}

type automationOutboxHandler struct {
	engine             *automation.Engine
	log                *logger.Logger
	merchantWebhooks   *crypto_worker.MerchantWebhookHandler
	checkoutReferences *crypto_worker.InvoiceCheckoutHandler
}

func (h *automationOutboxHandler) Handle(ctx context.Context, msg *postgres.OutboxMessage) error {
	if msg.AggregateType == crypto_worker.InvoiceCheckoutAggregateType {
		return h.checkoutReferences.Handle(ctx, msg)
	}
	if msg.AggregateType == crypto.MerchantWebhookAggregateType {
		return h.merchantWebhooks.Handle(ctx, msg)
	}
	var payload map[string]any
	if err := json.UnmarshalPreserveNumbers(msg.Payload, &payload); err != nil {
		h.log.Errorw("failed to unmarshal outbox payload", "error", err, "msg_id", msg.ID)
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	// Convert whole JSON numbers to int64 so templates and CEL do not see 1e+07.
	automation.SanitizePayloadNumbers(payload)

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

func (h *automationOutboxHandler) BackoffFor(msg *postgres.OutboxMessage) postgres.BackoffStrategy {
	if msg.AggregateType == crypto.MerchantWebhookAggregateType {
		return postgres.WebhookBackoff()
	}
	return nil
}

func (h *automationOutboxHandler) RetryPolicyFor(msg *postgres.OutboxMessage) postgres.RetryPolicy {
	if msg.AggregateType == crypto.MerchantWebhookAggregateType {
		return postgres.WebhookRetryPolicy(msg)
	}
	return postgres.RetryPolicy{MaxAttempts: 5}
}

func (w *MultiTenantWorker) cleanupSessions(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int, error) {
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	n, err := auth_repo.NewTokenRepo().CleanupExpiredTokens(ctx)
	if err != nil {
		return 0, err
	}

	if n > 0 {
		w.log.Infow("cleaned up expired sessions", "tenant_id", tenantID, "count", n)
	}
	return n, nil
}

func (w *MultiTenantWorker) cleanupIdempotency(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int, error) {
	result, err := pool.Exec(ctx, `
		UPDATE sys_idempotency
		SET archived_at = NOW(), application_result = '{}'::jsonb
		WHERE completed_at < NOW() - INTERVAL '90 days'
		  AND archived_at IS NULL
	`)
	if err != nil {
		return 0, err
	}
	n := int(result.RowsAffected())
	if n > 0 {
		w.log.Infow("archived idempotency results", "tenant_id", tenantID, "count", n)
	}
	return n, nil
}

func (w *MultiTenantWorker) cleanupAutomationHistory(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int, error) {
	result, err := pool.Exec(ctx, `
		DELETE FROM sys_automation_history 
		WHERE created_at < NOW() - INTERVAL '30 days'
	`)
	if err != nil {
		return 0, fmt.Errorf("cleanup automation history: %w", err)
	}
	n := int(result.RowsAffected())
	result, err = pool.Exec(ctx, `DELETE FROM sys_automation_runs
 WHERE status IN ('succeeded','failed','skipped') AND updated_at<clock_timestamp()-INTERVAL '30 days'`)
	if err != nil {
		return n, fmt.Errorf("cleanup scheduled run history: %w", err)
	}
	n += int(result.RowsAffected())
	if n > 0 {
		w.log.Infow("cleaned up automation history", "tenant_id", tenantID, "count", n)
	}
	return n, nil
}

func (w *MultiTenantWorker) cleanupNotifications(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int, error) {
	result, err := pool.Exec(ctx, `
		DELETE FROM sys_notifications 
		WHERE is_read = true AND created_at < NOW() - INTERVAL '30 days'
	`)
	if err != nil {
		return 0, fmt.Errorf("cleanup notifications: %w", err)
	}
	n := int(result.RowsAffected())
	if n > 0 {
		w.log.Infow("cleaned up old read notifications", "tenant_id", tenantID, "count", n)
	}
	return n, nil
}

func (w *MultiTenantWorker) cleanupAutomationFiles(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int, error) {
	result, err := pool.Exec(ctx, `
		DELETE FROM sys_automation_files 
		WHERE expires_at < NOW()
	`)
	if err != nil {
		return 0, fmt.Errorf("cleanup automation files: %w", err)
	}
	n := int(result.RowsAffected())
	if n > 0 {
		w.log.Infow("cleaned up expired automation files", "tenant_id", tenantID, "count", n)
	}
	return n, nil
}

func buildAccountProviderFactory(appEnv string) crypto_worker.AccountProviderFactory {
	return func(ctx context.Context, providerCode string, credsJSON []byte) (crypto.ExternalResourceProvider, error) {
		switch providerCode {
		case crypto.ResourceProviderITRX:
			var creds crypto_provider_account.ProviderCredentials
			if err := json.Unmarshal(credsJSON, &creds); err != nil {
				return nil, fmt.Errorf("unmarshal itrx credentials: %w", err)
			}
			return itrx.New(itrx.Config{
				BaseURL:     creds.BaseURL,
				APIKey:      creds.APIKey,
				APISecret:   creds.APISecret,
				CallbackURL: creds.CallbackURL,
				AppEnv:      appEnv,
			})
		default:
			return nil, fmt.Errorf("unsupported resource provider: %s", providerCode)
		}
	}
}

func getEnv(key, defaultValue string) string {
	value, err := secretfile.Get(key, defaultValue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration for %s: %v\n", key, err)
		os.Exit(1)
	}
	return value
}

func getEnvInt64(key string, defaultValue int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func mustEnv(key string) string {
	value := getEnv(key, "")
	if value == "" {
		fmt.Printf("required environment variable %s not set\n", key)
		os.Exit(1)
	}
	return value
}
