// Package v1 provides HTTP API version 1.
package v1

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/documents"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/stock"
	"metapus/internal/domain/reports"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/auth_repo"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/infrastructure/storage/postgres/report_repo"
	"metapus/internal/metadata"
	"metapus/pkg/logger"
)

// RouterConfig holds router configuration for multi-tenant architecture.
type RouterConfig struct {
	// TenantManager manages database connections for all tenants
	TenantManager *tenant.Manager

	// MetaPool is connection to meta-database (for health checks)
	MetaPool *pgxpool.Pool

	// Logger for request logging
	Logger *logger.Logger

	// JWTValidator for token validation
	JWTValidator middleware.JWTValidator

	// AuthService for authentication endpoints
	AuthService *auth.Service

	// Numerator for document number generation
	Numerator numerator.Generator

	// IdempotencyEnabled enables idempotency middleware
	IdempotencyEnabled bool
}

// NewRouter creates and configures the Gin router for multi-tenant architecture.
func NewRouter(cfg RouterConfig) *gin.Engine {
	// Set Gin mode based on environment
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// Global middleware (order matters!)
	router.Use(middleware.Recovery())
	router.Use(middleware.Trace())
	router.Use(middleware.Logger(cfg.Logger))
	router.Use(middleware.ErrorHandler())

	// Health endpoints (no auth, no tenant required)
	healthHandler := handlers.NewHealthHandlerMultiTenant(cfg.MetaPool, cfg.TenantManager)
	health := router.Group("/health")
	{
		health.GET("/live", healthHandler.Live)
		health.GET("/ready", healthHandler.Ready)
		health.GET("/info", healthHandler.Info)
		health.GET("/tenants", healthHandler.TenantsStats) // Admin endpoint for tenant stats
	}

	// Build metadata registry from factories (auto-registration)
	reg := metadata.NewRegistry()

	// API v1
	v1 := router.Group("/api/v1")
	{
		// Auth routes - need TenantDB middleware BEFORE auth
		registerAuthRoutes(v1, cfg)

		// Protected endpoints - TenantDB runs first, then Auth
		protected := v1.Group("")
		protected.Use(middleware.TenantDB(cfg.TenantManager)) // 1. Resolve tenant, get DB pool
		protected.Use(middleware.Auth(cfg.JWTValidator))      // 2. Validate JWT
		protected.Use(middleware.UserContext())               // 3. Add UserID to context for domain layer

		// Apply idempotency middleware for mutating operations
		if cfg.IdempotencyEnabled {
			protected.Use(idempotencyMiddleware(10 * time.Minute))
		}

		// Register entity routes (also populates metadata registry)
		registerCatalogRoutes(protected, cfg, reg)
		registerDocumentRoutes(protected, cfg, reg)
		registerRegisterRoutes(protected, cfg)
		registerReportRoutes(protected, cfg)
		registerMetaRoutes(protected, reg)
		registerUserPrefsRoutes(protected)
	}

	return router
}

// idempotencyMiddleware creates idempotency middleware that uses tenant pool + TxManager from context.
func idempotencyMiddleware(ttl time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		pool := tenant.MustGetPool(ctx)
		txm := postgres.MustGetTxManager(ctx)
		store := postgres.NewIdempotencyStoreFromRawPool(pool, txm, ttl)
		middleware.Idempotency(store)(c)
	}
}

// registerAuthRoutes registers authentication endpoints.
func registerAuthRoutes(rg *gin.RouterGroup, cfg RouterConfig) {
	if cfg.AuthService == nil {
		return
	}

	baseHandler := handlers.NewBaseHandler()
	authHandler := handlers.NewAuthHandler(baseHandler, cfg.AuthService)

	// Public auth endpoints (no JWT required, but need tenant for DB access)
	publicAuth := rg.Group("/auth")
	publicAuth.Use(middleware.TenantDB(cfg.TenantManager))

	// Protected auth endpoints (JWT required)
	protectedAuth := rg.Group("/auth")
	protectedAuth.Use(middleware.TenantDB(cfg.TenantManager))
	protectedAuth.Use(middleware.Auth(cfg.JWTValidator))

	authHandler.RegisterRoutes(publicAuth, protectedAuth)
}

// registerCatalogRoutes registers catalog (справочник) endpoints via the Abstract Factory registry.
// Also populates the metadata registry and builds refEndpoints map.
func registerCatalogRoutes(rg *gin.RouterGroup, cfg RouterConfig, reg *metadata.Registry) {
	catalogs := rg.Group("/catalog")

	deps := CatalogDeps{
		BaseHandler: handlers.NewBaseHandler(),
		Numerator:   cfg.Numerator,
	}

	// Build refEndpoints from factory declarations
	refEndpoints := map[string]string{
		"parent": "", // parent is self-referencing, skip
	}
	for _, factory := range catalogFactories {
		for _, refType := range factory.ReferenceTypes() {
			refEndpoints[refType] = "/catalog/" + factory.RoutePrefix()
		}
	}

	// Iterate over registered catalog factories
	for _, factory := range catalogFactories {
		handler := factory.Build(deps)
		RegisterCatalogRoutes(catalogs.Group("/"+factory.RoutePrefix()), handler, factory.Permission())

		// Register reference mappings: refType → entityName
		for _, refType := range factory.ReferenceTypes() {
			reg.RegisterReferenceMapping(refType, factory.EntityName())
		}

		// Auto-register metadata
		def := metadata.Inspect(factory.EntityStruct(), factory.EntityName(), metadata.TypeCatalog)
		def.Label = factory.EntityLabel()
		def.SetRefEndpoints(refEndpoints)
		reg.Register(def)
	}
}

// registerDocumentRoutes registers document endpoints via the Abstract Factory registry.
// Each document type is wired by its DocumentRegistration (see document_factory.go).
// Also populates the metadata registry.
func registerDocumentRoutes(rg *gin.RouterGroup, cfg RouterConfig, reg *metadata.Registry) {
	docsGroup := rg.Group("/document")

	// Create shared dependencies for all document types
	stockRepo := register_repo.NewStockRepo()
	stockService := stock.NewService(stockRepo)
	docLocker := postgres.NewDocLocker()
	postingEngine := posting.NewEngine(stockService, docLocker)

	contractRepo := catalog_repo.NewContractRepo()
	orgRepo := catalog_repo.NewOrganizationRepo()
	curRepo := catalog_repo.NewCurrencyRepo()
	currencyResolver := documents.NewCurrencyResolver(contractRepo, orgRepo, curRepo)

	deps := DocumentDeps{
		BaseHandler:      handlers.NewBaseHandler(),
		PostingEngine:    postingEngine,
		Numerator:        cfg.Numerator,
		CurrencyResolver: currencyResolver,
	}

	// Build refEndpoints from catalog factories for document metadata
	refEndpoints := map[string]string{
		"parent": "",
	}
	for _, factory := range catalogFactories {
		for _, refType := range factory.ReferenceTypes() {
			refEndpoints[refType] = "/catalog/" + factory.RoutePrefix()
		}
	}

	// Iterate over registered document factories
	for _, factory := range documentFactories {
		handler := factory.Build(deps)
		RegisterDocumentRoutes(docsGroup.Group("/"+factory.RoutePrefix()), handler, factory.Permission())

		// Auto-register metadata
		def := metadata.Inspect(factory.EntityStruct(), factory.EntityName(), metadata.TypeDocument)
		def.Label = factory.EntityLabel()
		def.SetRefEndpoints(refEndpoints)
		reg.Register(def)
	}
}

// registerRegisterRoutes registers accumulation register endpoints.
func registerRegisterRoutes(rg *gin.RouterGroup, cfg RouterConfig) {
	registers := rg.Group("/registers")
	baseHandler := handlers.NewBaseHandler()

	// Stock register
	{
		stockRepo := register_repo.NewStockRepo()
		stockService := stock.NewService(stockRepo)
		stockHandler := handlers.NewStockHandler(baseHandler, stockService, stockRepo)

		stockGroup := registers.Group("/stock")
		stockGroup.GET("/balances", middleware.RequirePermission("register:stock:read"), stockHandler.GetBalances)
		stockGroup.GET("/movements", middleware.RequirePermission("register:stock:read"), stockHandler.GetMovements)
		stockGroup.GET("/turnovers", middleware.RequirePermission("register:stock:read"), stockHandler.GetTurnovers)
		stockGroup.GET("/availability/:productId", middleware.RequirePermission("register:stock:read"), stockHandler.GetProductAvailability)
	}
}

// registerMetaRoutes registers metadata/schema endpoints.
func registerMetaRoutes(rg *gin.RouterGroup, reg *metadata.Registry) {
	handler := handlers.NewMetadataHandler(reg)
	meta := rg.Group("/meta")
	{
		meta.GET("", handler.ListEntities)
		meta.GET("/:name", handler.GetEntity)
		meta.GET("/:name/filters", handler.GetEntityFilters)
	}
}

// registerReportRoutes registers report endpoints.
func registerReportRoutes(rg *gin.RouterGroup, cfg RouterConfig) {
	reportsGroup := rg.Group("/reports")
	baseHandler := handlers.NewBaseHandler()

	reportRepo := report_repo.NewReportRepo()
	reportService := reports.NewService(reportRepo)
	reportHandler := handlers.NewReportsHandler(baseHandler, reportService)

	reportsGroup.GET("/stock-balance", middleware.RequirePermission("report:stock:read"), reportHandler.GetStockBalance)
	reportsGroup.GET("/stock-turnover", middleware.RequirePermission("report:stock:read"), reportHandler.GetStockTurnover)
	reportsGroup.GET("/document-journal", middleware.RequirePermission("report:documents:read"), reportHandler.GetDocumentJournal)
}

// registerUserPrefsRoutes registers user preferences endpoints.
func registerUserPrefsRoutes(rg *gin.RouterGroup) {
	baseHandler := handlers.NewBaseHandler()
	repo := auth_repo.NewUserPrefsRepo()
	handler := handlers.NewUserPrefsHandler(baseHandler, repo)
	handler.RegisterRoutes(rg)
}
