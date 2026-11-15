// Package v1 provides HTTP API version 1.
package v1

import (
	"context"
	"net/http"
	"net/netip"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/application/integrationapp"
	"metapus/internal/application/readaccess"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/documents"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/security_profile"
	_ "metapus/internal/infrastructure/http/jsoncodec"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/search"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/internal/metadata"
	"metapus/pkg/logger"
)

// RouterConfig holds router configuration for multi-tenant architecture.
type RouterConfig struct {
	PDFRenderer printing.PDFRenderer
	PDFReady    func(context.Context) error
	// TenantManager manages database connections for all tenants
	TenantManager *tenantdb.Manager

	// MetaPool is connection to meta-database (for health checks)
	MetaPool *pgxpool.Pool

	// Logger for request logging
	Logger *logger.Logger

	// JWTValidator for token validation
	JWTValidator middleware.JWTValidator

	// AuthSvc for authentication endpoints
	AuthSvc *auth.Service

	// MFARepo verifies step-up confirmations.
	MFARepo auth.MFARepository

	// Admin security controls for admin routes.
	AdminMFAMode      auth.MFAMode
	AdminSecurityMode auth.AdminSecurityMode
	AdminAllowedCIDRs []netip.Prefix

	// Numerator for document number generation
	Numerator numerator.Generator

	// ProfileProvider provides cached security profiles for RLS/FLS
	ProfileProvider security_profile.ProfileProvider

	// PolicyEngine for CEL-based fine-grained authorization (optional)
	PolicyEngine *security.PolicyEngine

	// EventLogRepo for event logging in middleware and handlers (optional)
	EventLogRepo *postgres.EventLogRepo

	// Registry holds all entity factory registrations (catalogs, documents).
	// If nil, built-in defaults are used via RegisterDefaults().
	Registry *FactoryRegistry

	// PostingEngine is the document posting engine (optional).
	// If nil, a default engine with built-in recorders (settlement) is created.
	// Inject a custom engine to add new register types or wrap with logging/metrics.
	PostingEngine *posting.Engine

	// PostingModules install configuration registers into the posting engine
	// and expose their document movements.
	PostingModules []posting.Module

	// RouteModules mount configuration routes after the platform routes.
	RouteModules []RouteModule

	// IntegrationProber tests provider connections from the integrations admin
	// API. Nil disables connection tests.
	IntegrationProber integrationapp.ConnectionProber

	// CurrencyResolver resolves document currency (optional).
	// If nil, the default 1C-style chain resolver is created (Document → Contract → Org → System).
	CurrencyResolver domain.CurrencyResolveStrategy

	// CurrencyMetadataResolver resolves currency metadata (decimalPlaces, symbol).
	// Used by outbox decorators to enrich automation events.
	CurrencyMetadataResolver domain.CurrencyMetadataResolver

	// Version is the server binary version (set via ldflags).
	Version string

	// BuildTime is the server build timestamp (set via ldflags).
	BuildTime string

	// WSTicketStore for WebSocket ticket-based authentication.
	WSTicketStore *auth.WSTicketStore
}

// NewRouter creates and configures the Gin router for multi-tenant architecture.
func NewRouter(cfg RouterConfig) *gin.Engine {
	// Set Gin mode based on environment
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	configureTrustedProxies(router, cfg.Logger)

	// Ensure EventLogRepo is available
	eventLogRepo := cfg.EventLogRepo
	if eventLogRepo == nil {
		eventLogRepo = postgres.NewEventLogRepo()
	}
	cfg.EventLogRepo = eventLogRepo

	// Wire event logging for permission middleware
	middleware.SetPermissionEventWriter(eventLogRepo)

	// Global middleware (order matters!)
	router.Use(middleware.CORS())
	router.Use(middleware.Recovery(eventLogRepo))
	router.Use(middleware.Trace())
	router.Use(middleware.Logger(cfg.Logger, eventLogRepo))
	router.Use(middleware.ErrorHandler())

	// Health endpoints (no auth, no tenant required)
	healthHandler := handlers.NewHealthHandlerMultiTenant(cfg.MetaPool, cfg.TenantManager, cfg.Version)
	health := router.Group("/health")
	{
		health.GET("/live", healthHandler.Live)
		health.GET("/ready", healthHandler.Ready)
		health.GET("/capabilities/pdf", func(c *gin.Context) {
			if cfg.PDFReady == nil || cfg.PDFReady(c.Request.Context()) != nil {
				c.Status(http.StatusServiceUnavailable)
				return
			}
			c.Status(http.StatusOK)
		})
		health.GET("/info", healthHandler.Info)
		// NOTE: /tenants moved to admin group (prevents unauthenticated tenant enumeration)
	}

	// Use provided factory registry — composition root must configure this
	factoryReg := cfg.Registry
	if factoryReg == nil {
		panic("v1.NewRouter: cfg.Registry must not be nil — use content.RegisterDefaults(reg) in main.go")
	}

	// Build metadata registry from factories (auto-registration)
	reg := metadata.NewRegistry()

	// API v1
	var moduleContext RouteContext
	v1 := router.Group("/api/v1")
	{
		// Auth routes - need TenantDB middleware BEFORE auth
		registerAuthRoutes(v1, cfg, eventLogRepo)

		// Public system routes (no auth, no tenant needed)
		versionHandler := handlers.NewSystemVersionHandler(cfg.Version, cfg.BuildTime)
		v1.GET("/system/version", versionHandler.Version)

		// Protected endpoints - TenantDB runs first, then Auth
		protected := v1.Group("")
		protected.Use(middleware.TenantDB(cfg.TenantManager)) // 1. Resolve tenant, get DB pool
		protected.Use(middleware.Auth(cfg.JWTValidator))      // 2. Validate JWT
		protected.Use(middleware.AdminGuard(cfg.AdminSecurityMode, cfg.AdminAllowedCIDRs, cfg.AdminMFAMode, cfg.MFARepo, eventLogRepo))
		// Security profiles are mandatory — fail-fast if misconfigured.
		if cfg.ProfileProvider == nil {
			panic("v1.NewRouter: cfg.ProfileProvider must not be nil — security profiles are required for DataScope")
		}
		protected.Use(middleware.SecurityContext(cfg.ProfileProvider))
		reads := postgres.NewAuthorizedReadService(reg, cfg.PolicyEngine)
		protected.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(readaccess.WithService(c.Request.Context(), reads))
			c.Next()
		})

		// Pre-create CurrencyResolver so catalog hooks can register invalidation,
		// and document services can use it for resolution. This must happen BEFORE
		// registerCatalogRoutes so Contract/Organization hooks wire up correctly.
		if cfg.CurrencyResolver == nil {
			contractRepo := catalog_repo.NewContractRepo()
			orgRepo := catalog_repo.NewOrganizationRepo()
			curRepo := catalog_repo.NewCurrencyRepo()
			cfg.CurrencyResolver = documents.NewCurrencyResolver(contractRepo, orgRepo, curRepo)
		}

		// Extract optional cache invalidator for catalog hooks
		var currencyInvalidator domain.CurrencyCacheInvalidator
		if inv, ok := cfg.CurrencyResolver.(domain.CurrencyCacheInvalidator); ok {
			currencyInvalidator = inv
		}

		// Register entity routes (also populates metadata registry)
		registerCatalogRoutes(protected, cfg, factoryReg, reg, eventLogRepo, currencyInvalidator)
		registerDocumentRoutes(protected, cfg, factoryReg, reg, eventLogRepo)
		registerRegisterRoutes(protected, cfg, factoryReg, reg)
		reportCompiler := registerReportRoutes(protected, cfg, factoryReg, reg)
		registerMetaRoutes(protected, reg)
		registerRefResolverRoutes(protected, reg)
		registerUserPrefsRoutes(protected)
		registerListViewRoutes(protected)
		registerSettingsRoutes(protected, cfg)
		registerSecurityRoutes(protected, cfg)

		// WebSocket group — TenantDB only, no JWT (ticket-based auth in handler).
		// Must be separate from `protected` to avoid Auth middleware blocking the upgrade.
		wsGroup := v1.Group("")
		wsGroup.Use(middleware.TenantDB(cfg.TenantManager))

		sysGroup := protected.Group("/system")
		sysGroup.Use(middleware.RequireRole("admin"))
		registerSystemRoutes(protected, sysGroup, wsGroup, cfg, eventLogRepo, reg, cfg.WSTicketStore, reportCompiler)
		moduleContext = RouteContext{Root: router, API: v1, Protected: protected, System: sysGroup, Config: cfg, Metadata: reg}

		// Global data search (Ctrl+K) — available to all authenticated users.
		// Must be registered after entity routes so metadata.Registry is populated.
		searchSvc := search.NewService(reg, reads)
		searchHandler := handlers.NewGlobalSearchHandler(searchSvc)
		protected.GET("/search", searchHandler.Search)

		// Entity preview (Command Palette → ArrowRight) — single entity preview card.
		previewHandler := handlers.NewEntityPreviewHandler(searchSvc)
		protected.GET("/search/preview", previewHandler.Preview)

		// Stateless XLSX renderer for document table parts (no entity binding needed).
		protected.POST("/export-table-part", handlers.ExportTablePart)
	}

	// Tenant monitoring is protected by the regular tenant-aware admin session.
	adminAuthGroup := v1.Group("")
	adminAuthGroup.Use(middleware.TenantDB(cfg.TenantManager))
	adminAuthGroup.Use(middleware.Auth(cfg.JWTValidator))
	adminAuthGroup.Use(middleware.AdminGuard(cfg.AdminSecurityMode, cfg.AdminAllowedCIDRs, cfg.AdminMFAMode, cfg.MFARepo, eventLogRepo))
	registerAdminTenantRoutes(adminAuthGroup, cfg, healthHandler)

	// Configuration routes (separate prefixes and /api/v1 additions).
	for _, module := range cfg.RouteModules {
		module.RegisterRoutes(moduleContext)
	}

	return router
}

func configureTrustedProxies(router *gin.Engine, log *logger.Logger) {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	var proxies []string
	if raw != "" {
		for part := range strings.SplitSeq(raw, ",") {
			proxy := strings.TrimSpace(part)
			if proxy != "" {
				proxies = append(proxies, proxy)
			}
		}
	}
	if err := router.SetTrustedProxies(proxies); err != nil && log != nil {
		log.Errorw("invalid TRUSTED_PROXIES", "error", err)
		panic("invalid TRUSTED_PROXIES: " + err.Error())
	} else if err != nil {
		panic("invalid TRUSTED_PROXIES: " + err.Error())
	}
}
