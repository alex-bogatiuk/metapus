// Package v1 provides HTTP API version 1.
package v1

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/catalogs/nomenclature"
	"metapus/internal/domain/catalogs/unit"
	"metapus/internal/domain/catalogs/warehouse"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/domain/documents/inventory"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/stock"
	"metapus/internal/domain/reports"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/infrastructure/storage/postgres/report_repo"
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

	// API v1
	v1 := router.Group("/api/v1")
	{
		// Auth routes - need TenantDB middleware BEFORE auth
		registerAuthRoutes(v1, cfg)

		// Protected endpoints - TenantDB runs first, then Auth
		protected := v1.Group("")
		protected.Use(middleware.TenantDB(cfg.TenantManager)) // 1. Resolve tenant, get DB pool
		protected.Use(middleware.Auth(cfg.JWTValidator))      // 2. Validate JWT

		// Apply idempotency middleware for mutating operations
		if cfg.IdempotencyEnabled {
			protected.Use(idempotencyMiddleware(10 * time.Minute))
		}

		// Register entity routes
		registerCatalogRoutes(protected, cfg)
		registerDocumentRoutes(protected, cfg)
		registerRegisterRoutes(protected, cfg)
		registerReportRoutes(protected, cfg)
		registerMetaRoutes(protected, cfg)
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

// registerCatalogRoutes registers catalog (справочник) endpoints.
func registerCatalogRoutes(rg *gin.RouterGroup, cfg RouterConfig) {
	catalogs := rg.Group("/catalog")
	baseHandler := handlers.NewBaseHandler()

	// Note: Repos and services are created once but TxManager is obtained from context per-request

	// --- COUNTERPARTIES ---
	{
		repo := catalog_repo.NewCounterpartyRepo()
		service := counterparty.NewService(repo, cfg.Numerator)
		handler := handlers.NewCounterpartyHandler(baseHandler, service)
		RegisterCatalogRoutes(catalogs.Group("/counterparties"), handler, "catalog:counterparty")
	}

	// --- NOMENCLATURE ---
	{
		repo := catalog_repo.NewNomenclatureRepo()
		service := nomenclature.NewService(repo, cfg.Numerator)
		handler := handlers.NewNomenclatureHandler(baseHandler, service)
		RegisterCatalogRoutes(catalogs.Group("/nomenclature"), handler, "catalog:nomenclature")
	}

	// --- WAREHOUSES ---
	{
		repo := catalog_repo.NewWarehouseRepo()
		service := warehouse.NewService(repo, cfg.Numerator)
		handler := handlers.NewWarehouseHandler(baseHandler, service)
		RegisterCatalogRoutes(catalogs.Group("/warehouses"), handler, "catalog:warehouse")
	}

	// --- UNITS ---
	{
		repo := catalog_repo.NewUnitRepo()
		service := unit.NewService(repo, cfg.Numerator)
		handler := handlers.NewUnitHandler(baseHandler, service)
		RegisterCatalogRoutes(catalogs.Group("/units"), handler, "catalog:unit")
	}

	// --- CURRENCIES ---
	{
		repo := catalog_repo.NewCurrencyRepo()
		service := currency.NewService(repo, cfg.Numerator)
		handler := handlers.NewCurrencyHandler(baseHandler, service)
		RegisterCatalogRoutes(catalogs.Group("/currencies"), handler, "catalog:currency")
	}
}

// registerDocumentRoutes registers document endpoints.
func registerDocumentRoutes(rg *gin.RouterGroup, cfg RouterConfig) {
	documents := rg.Group("/document")
	baseHandler := handlers.NewBaseHandler()

	// Create shared dependencies for documents
	stockRepo := register_repo.NewStockRepo()
	stockService := stock.NewService(stockRepo)
	postingEngine := posting.NewEngine(stockService)

	// --- GOODS RECEIPT ---
	{
		repo := document_repo.NewGoodsReceiptRepo()
		service := goods_receipt.NewService(repo, postingEngine, cfg.Numerator, nil)
		handler := handlers.NewGoodsReceiptHandler(baseHandler, service)
		RegisterDocumentRoutes(documents.Group("/goods-receipt"), handler, "document:goods_receipt")
	}

	// --- GOODS ISSUE ---
	{
		repo := document_repo.NewGoodsIssueRepo()
		service := goods_issue.NewService(repo, postingEngine, cfg.Numerator, nil)
		handler := handlers.NewGoodsIssueHandler(baseHandler, service)
		RegisterDocumentRoutes(documents.Group("/goods-issue"), handler, "document:goods_issue")
	}

	// --- INVENTORY (with custom routes) ---
	{
		repo := document_repo.NewInventoryRepo()
		service := inventory.NewService(repo, postingEngine, stockService, cfg.Numerator, nil)
		handler := handlers.NewInventoryHandler(baseHandler, service)

		// Register standard document routes
		invGroup := documents.Group("/inventory")
		RegisterDocumentRoutes(invGroup, handler, "document:inventory")

		// Register custom inventory-specific routes
		invGroup.POST("/:id/prepare-sheet", middleware.RequirePermission("document:inventory:update"), handler.PrepareSheet)
		invGroup.POST("/:id/start", middleware.RequirePermission("document:inventory:update"), handler.Start)
		invGroup.POST("/:id/record-count", middleware.RequirePermission("document:inventory:update"), handler.RecordCount)
		invGroup.POST("/:id/complete", middleware.RequirePermission("document:inventory:update"), handler.Complete)
		invGroup.POST("/:id/cancel", middleware.RequirePermission("document:inventory:update"), handler.Cancel)
		invGroup.GET("/:id/comparison", middleware.RequirePermission("document:inventory:read"), handler.GetComparison)
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
func registerMetaRoutes(rg *gin.RouterGroup, cfg RouterConfig) {
	meta := rg.Group("/meta")
	{
		// GET /api/v1/meta/schemas/{type}/{name}    - Get entity schema
		// GET /api/v1/meta/layouts/{entity}         - Get UI layout hints
		_ = meta
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
