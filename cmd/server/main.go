// Package main is the entry point for the Metapus API server.
// Multi-tenant architecture: Database-per-Tenant.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/content"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/security_profile"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres/auth_repo"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/storage/postgres/security_repo"
	"metapus/pkg/logger"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/server
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	// Initialize logger
	log, err := logger.New(logger.Config{
		Level:       getEnv("LOG_LEVEL", "info"),
		Development: getEnv("APP_ENV", "development") == "development",
	})
	if err != nil {
		fmt.Printf("failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	log.Infow("starting metapus server",
		"version", Version,
		"build_time", BuildTime,
		"mode", "multi-tenant",
	)

	// --- Meta-database connection ---
	metaDSN := mustEnv("META_DATABASE_URL")
	metaPool, err := pgxpool.New(ctx, metaDSN)
	if err != nil {
		log.Fatalw("failed to connect to meta database", "error", err)
	}
	defer metaPool.Close()

	if err := metaPool.Ping(ctx); err != nil {
		log.Fatalw("failed to ping meta database", "error", err)
	}
	log.Info("meta database connection established")

	// --- Tenant Registry and Manager ---
	registry := tenant.NewPostgresRegistry(metaPool)

	managerCfg := tenant.DefaultManagerConfig()
	managerCfg.DBUser = mustEnv("TENANT_DB_USER")
	managerCfg.DBPassword = mustEnv("TENANT_DB_PASSWORD")

	// Cloud mode: restrict this instance to serve only tenants in this version group.
	if vg := getEnv("VERSION_GROUP", ""); vg != "" {
		managerCfg.VersionGroup = vg
		log.Infow("cloud mode: version group filter enabled", "version_group", vg)
	}

	// Optional configuration overrides
	if maxPools := getEnvInt("TENANT_MAX_POOLS", 100); maxPools > 0 {
		managerCfg.MaxTotalPools = maxPools
	}
	if maxConns := getEnvInt("TENANT_MAX_CONNS_PER_POOL", 10); maxConns > 0 {
		managerCfg.MaxConnsPerTenant = int32(maxConns)
	}
	if idleTimeout := getEnvDuration("TENANT_POOL_IDLE_TIMEOUT", 30*time.Minute); idleTimeout > 0 {
		managerCfg.PoolIdleTimeout = idleTimeout
	}

	tenantManager := tenant.NewManager(managerCfg, registry, log)
	defer tenantManager.Close()

	log.Infow("tenant manager initialized",
		"max_pools", managerCfg.MaxTotalPools,
		"max_conns_per_tenant", managerCfg.MaxConnsPerTenant,
		"idle_timeout", managerCfg.PoolIdleTimeout,
	)

	// Optional: Prewarm pools for known tenants
	if getEnv("PREWARM_POOLS", "false") == "true" {
		log.Info("prewarming tenant pools...")
		if err := tenantManager.PrewarmPools(ctx); err != nil {
			log.Warnw("failed to prewarm some pools", "error", err)
		}
	}

	// --- Migration State Store ---
	// Self-contained table in meta-database for pre-update version snapshots.
	migrationStateStore := tenant.NewPostgresMigrationStateStore(metaPool)
	if err := migrationStateStore.EnsureTable(ctx); err != nil {
		log.Fatalw("failed to ensure migration state table", "error", err)
	}

	// Recover tenants stuck in "updating" from a previous crash.
	migration.RecoverStuckTenants(ctx, registry, log)

	// --- JWT Service ---
	jwtSecret := mustEnv("JWT_SECRET")
	jwtConfig := auth.DefaultJWTConfig(jwtSecret)
	jwtService := auth.NewJWTService(jwtConfig)

	// --- Auth Service ---
	// Note: Auth repos will get TxManager from context per-request
	userRepo := auth_repo.NewUserRepo()
	roleRepo := auth_repo.NewRoleRepo()
	permRepo := auth_repo.NewPermissionRepo()
	tokenRepo := auth_repo.NewTokenRepo()

	authConfig := auth.DefaultServiceConfig()
	authService := auth.NewService(
		userRepo,
		roleRepo,
		permRepo,
		tokenRepo,
		nil, // TxManager will come from context
		jwtService,
		authConfig,
	)

	// --- Numerator Service ---
	numeratorService := numerator.New()

	// --- Security Profile Provider (cached) ---
	profileRepo := security_repo.NewProfileRepo()
	profileCacheTTL := getEnvDuration("SECURITY_PROFILE_CACHE_TTL", 5*time.Minute)
	profileProvider := security_profile.NewCachedProfileProvider(profileRepo, profileCacheTTL)

	// --- CEL Policy Engine ---
	policyEngine, err := security.NewPolicyEngine()
	if err != nil {
		log.Fatal("failed to create policy engine", "error", err)
	}
	defer policyEngine.Stop()

	// --- Entity Registry (composition root) ---
	// Create factory registry and populate with built-in business content.
	factoryReg := v1.NewFactoryRegistry()
	content.RegisterDefaults(factoryReg)

	// Client extensions — uncomment to enable:
	// vehicle.Register(factoryReg, platform.ExtensionConfig{})

	// --- WebSocket Ticket Store (F-05) ---
	wsTicketStore := auth.NewWSTicketStore()
	defer wsTicketStore.Stop()

	// --- Router ---
	router := v1.NewRouter(v1.RouterConfig{
		TenantManager:       tenantManager,
		MetaPool:            metaPool,
		Logger:              log,
		JWTValidator:        jwtService,
		AuthService:         authService,
		Numerator:           numeratorService,
		IdempotencyEnabled:  getEnv("IDEMPOTENCY_ENABLED", "false") == "true",
		ProfileProvider:     profileProvider,
		PolicyEngine:        policyEngine,
		Registry:            factoryReg,
		Version:             Version,
		BuildTime:           BuildTime,
		MigrationStateStore: migrationStateStore,
		WSTicketStore:       wsTicketStore,
	})

	// --- HTTP Server ---
	port := getEnv("APP_PORT", "8080")
	server := &http.Server{
		Addr:           ":" + port,
		Handler:        http.MaxBytesHandler(router, 10<<20), // F-17: 10 MiB global body limit
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // F-17: 1 MiB max header size
	}

	// Start server in goroutine
	go func() {
		log.Infow("server starting", "port", port, "mode", "multi-tenant")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalw("server failed", "error", err)
		}
	}()

	// --- Graceful shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down server...")

	// Give outstanding requests 30 seconds to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalw("server forced to shutdown", "error", err)
	}

	log.Info("server stopped")
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

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
			return result
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
