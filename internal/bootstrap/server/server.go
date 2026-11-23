// Package server runs the Metapus API server for a configuration.
// Multi-tenant architecture: Database-per-Tenant.
//
// It is the platform's composition root: it builds the dependency graph,
// asks the configuration for its part (configuration.Configuration.Server)
// and serves HTTP. Each configuration's cmd/ main calls Main with itself.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/domain/printing"
	"github.com/alex-bogatiuk/metapus/internal/domain/security_profile"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/authjwt"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/authqr"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/numerator"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/pdfrender"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/auth_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/security_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
	platformtelemetry "github.com/alex-bogatiuk/metapus/internal/platform/telemetry"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

// Build identifies the running binary; mains set it from ldflags:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/server
type Build struct {
	Version   string
	BuildTime string
}

// Main runs the API server for cfg and the client extensions until
// SIGINT/SIGTERM, and exits the process on a configuration or startup error.
func Main(build Build, cfg configuration.Configuration, extensions ...func(*v1.FactoryRegistry)) {
	if err := configuration.CheckAPIVersion(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	settings, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration:\n%v\n", err)
		os.Exit(1)
	}
	log, err := logger.New(logger.Config{Level: settings.LogLevel, Development: settings.development()})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	if err := run(context.Background(), settings, build, cfg, extensions, log); err != nil {
		log.Errorw("server failed", "error", err)
		_ = log.Sync()
		os.Exit(1)
	}
}

// run builds the server and serves until SIGINT/SIGTERM or a listener
// failure. Every resource it opens is closed by a defer, so an error returned
// from any step still releases what earlier steps acquired. ctx is the
// lifetime of the built components; it is not cancelled by the signal, so
// background work keeps running while in-flight requests drain.
func run(ctx context.Context, cfg config, build Build, app configuration.Configuration, extensions []func(*v1.FactoryRegistry), log *logger.Logger) error {
	shutdownTracing, err := platformtelemetry.ConfigureTracing(ctx, "metapus-api", build.Version, os.Getenv)
	if err != nil {
		return fmt.Errorf("initialize OTLP tracing: %w", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if shutdownErr := shutdownTracing(shutdownCtx); shutdownErr != nil {
			log.Errorw("failed to flush OTLP traces", "error", shutdownErr)
		}
	}()
	log.Infow("starting metapus server",
		"version", build.Version,
		"build_time", build.BuildTime,
		"mode", "multi-tenant",
		"configuration", app.Name(),
	)

	// --- Meta-database connection ---
	metaPool, err := postgres.OpenPool(ctx, cfg.MetaDatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to meta database: %w", err)
	}
	defer metaPool.Close()

	if err := metaPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping meta database: %w", err)
	}
	log.Info("meta database connection established")

	// --- Tenant Registry and Manager ---
	registry := tenantdb.NewPostgresRegistry(metaPool)

	managerCfg := tenantdb.DefaultManagerConfig()
	managerCfg.DBUser = cfg.TenantDBUser
	managerCfg.DBPassword = cfg.TenantDBPassword
	managerCfg.ConfigurePool = postgres.ConfigurePool
	managerCfg.MaxTotalPools = cfg.TenantMaxPools
	managerCfg.MaxConnsPerTenant = int32(cfg.TenantMaxConnsPerPool)
	managerCfg.PoolIdleTimeout = cfg.TenantPoolIdleTimeout

	tenantManager := tenantdb.NewManager(managerCfg, registry, log)
	defer tenantManager.Close()

	log.Infow("tenant manager initialized",
		"max_pools", managerCfg.MaxTotalPools,
		"max_conns_per_tenant", managerCfg.MaxConnsPerTenant,
		"idle_timeout", managerCfg.PoolIdleTimeout,
	)

	if cfg.PrewarmPools {
		log.Info("prewarming tenant pools...")
		if err := tenantManager.PrewarmPools(ctx); err != nil {
			log.Warnw("failed to prewarm some pools", "error", err)
		}
	}

	// --- JWT Service ---
	jwtSvc := authjwt.NewJWTService(authjwt.DefaultJWTConfig(cfg.JWTSecret))

	// --- Auth Service ---
	// Note: Auth repos will get TxManager from context per-request
	userRepo := auth_repo.NewUserRepo()
	roleRepo := auth_repo.NewRoleRepo()
	permRepo := auth_repo.NewPermissionRepo()
	tokenRepo := auth_repo.NewTokenRepo()
	sessionRepo := auth_repo.NewSessionRepo()
	mfaRepo := auth_repo.NewMFARepo()
	eventLogRepo := postgres.NewEventLogRepo()
	notificationRepo := postgres.NewNotificationRepo()
	authStateCache := auth.NewAuthStateCache(cfg.AuthStateCacheTTL, cfg.AuthStateCacheMaxEntries)
	accessValidator := auth.NewAccessTokenValidator(jwtSvc, sessionRepo, authStateCache)

	// --- Numerator Service ---
	numeratorSvc := numerator.New()

	// --- The configuration's part ---
	expectedSchema, err := configuration.ExpectedSchema(app)
	if err != nil {
		return err
	}
	part, err := app.Server(ctx, configuration.ServerEnv{
		Logger:    log,
		MetaPool:  metaPool,
		Tenants:   registry,
		Numerator: numeratorSvc,
		EventLog:  eventLogRepo,
	})
	if err != nil {
		return fmt.Errorf("build the %s configuration's server part: %w", app.Name(), err)
	}
	authOptions := []auth.ServiceOption{auth.WithQRCodeEncoder(authqr.Encoder{})}
	if part.AccessScopes != nil {
		authOptions = append(authOptions, auth.WithAccessScopeProvider(part.AccessScopes))
	}

	authSvc := auth.NewService(
		userRepo,
		roleRepo,
		permRepo,
		tokenRepo,
		mfaRepo,
		sessionRepo,
		authStateCache,
		nil, // TxManager will come from context
		jwtSvc,
		eventLogRepo,
		notificationRepo,
		cfg.Auth,
		authOptions...,
	)

	// --- Security Profile Provider (cached) ---
	profileProvider := security_profile.NewCachedProfileProvider(
		security_repo.NewProfileRepo(),
		cfg.SecurityProfileCacheTTL,
		cfg.SecurityProfileCacheMaxEntries,
	)

	// --- CEL Policy Engine ---
	policyEngine, err := security.NewPolicyEngine()
	if err != nil {
		return fmt.Errorf("create policy engine: %w", err)
	}
	defer policyEngine.Stop()

	// --- WebSocket Ticket Store ---
	wsTicketStore := auth.NewWSTicketStore()
	defer wsTicketStore.Stop()

	// Entity factories: the platform's, the configuration's (with the
	// services its part built), then client extensions.
	assembled := configuration.WithRegister(app, part.Register)
	factoryReg := configuration.Compose(assembled, extensions...)

	// --- Router ---
	var pdfRenderPort printing.PDFRenderer
	var pdfReady func(context.Context) error
	if cfg.PDFRendererURL != "" {
		pdfClient, err := pdfrender.NewClient(cfg.PDFRendererURL)
		if err != nil {
			return fmt.Errorf("invalid PDF renderer configuration: %w", err)
		}
		pdfRenderPort = pdfClient
		pdfReady = pdfClient.Ready
	}
	router := v1.NewRouter(v1.RouterConfig{
		PostingModules:             part.PostingModules,
		PDFRenderer:                pdfRenderPort,
		PDFReady:                   pdfReady,
		TenantManager:              tenantManager,
		MetaPool:                   metaPool,
		Logger:                     log,
		JWTValidator:               accessValidator,
		AuthSvc:                    authSvc,
		MFARepo:                    mfaRepo,
		AdminMFAMode:               cfg.Auth.AdminMFAMode,
		AdminSecurityMode:          cfg.Auth.AdminSecurityMode,
		AdminAllowedCIDRs:          cfg.Auth.AdminAllowedCIDRs,
		Numerator:                  numeratorSvc,
		ProfileProvider:            profileProvider,
		PolicyEngine:               policyEngine,
		EventLogRepo:               eventLogRepo,
		Registry:                   factoryReg,
		Version:                    build.Version,
		BuildTime:                  build.BuildTime,
		WSTicketStore:              wsTicketStore,
		IntegrationConnectionTests: part.IntegrationConnectionTests,
		IntegrationCatalog:         configuration.IntegrationCatalog(app),
		ExpectedSchema:             expectedSchema,
		RouteModules:               part.RouteModules,
	})

	// --- HTTP Server ---
	server := &http.Server{
		Addr:           ":" + cfg.Port,
		Handler:        http.MaxBytesHandler(router, 10<<20), // 10 MiB global body limit
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MiB max header size
	}
	return serve(ctx, server, log)
}

// serve runs server until SIGINT/SIGTERM, then gives outstanding requests
// 30 seconds to finish. A listener failure is returned instead of exiting, so
// run's deferred cleanup still executes.
func serve(ctx context.Context, server *http.Server, log *logger.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Infow("server starting", "addr", server.Addr, "mode", "multi-tenant")
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("server stopped")
	return nil
}
