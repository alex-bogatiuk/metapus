// Package v1 provides HTTP API version 1.
package v1

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/entity"
	"metapus/internal/core/eventlog"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/documents"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/registers/cost"
	"metapus/internal/domain/registers/settlement"
	"metapus/internal/domain/registers/stock"
	"metapus/internal/domain/reports/compiler"
	"metapus/internal/domain/listview"
	"metapus/internal/domain/reports/variants"
	"metapus/internal/domain/search"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/cache"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/auth_repo"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/infrastructure/storage/postgres/security_repo"
	"metapus/internal/metadata"
	"metapus/internal/platform"
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
	// If nil, a default engine with built-in recorders (stock, cost, settlement) is created.
	// Inject a custom engine to add new register types or wrap with logging/metrics.
	PostingEngine *posting.Engine

	// CurrencyResolver resolves document currency (optional).
	// If nil, the default 1C-style chain resolver is created (Document → Contract → Org → System).
	CurrencyResolver domain.CurrencyResolveStrategy

	// CurrencyMetadataResolver resolves currency metadata (decimalPlaces, symbol).
	// Used by outbox decorators to enrich automation events.
	CurrencyMetadataResolver domain.CurrencyMetadataResolver

	// SchemaCache for metadata-driven features (optional).
	// Provides custom fields merge with static metadata.
	SchemaCache *cache.SchemaCache

	// Version is the server binary version (set via ldflags).
	Version string

	// BuildTime is the server build timestamp (set via ldflags).
	BuildTime string

	// MigrationStateStore manages pre-update version persistence for rollback support.
	// Created in main.go, backed by meta-database.
	MigrationStateStore tenant.MigrationStateStore

	// WSTicketStore for WebSocket ticket-based authentication.
	WSTicketStore *auth.WSTicketStore
}

// NewRouter creates and configures the Gin router for multi-tenant architecture.
func NewRouter(cfg RouterConfig) *gin.Engine {
	// Set Gin mode based on environment
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// Ensure EventLogRepo is available
	eventLogRepo := cfg.EventLogRepo
	if eventLogRepo == nil {
		eventLogRepo = postgres.NewEventLogRepo()
	}

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
		health.GET("/info", healthHandler.Info)
		// NOTE: /tenants moved to admin group (prevents unauthenticated tenant enumeration)
	}

	// Internal endpoints (for reverse proxy, not exposed publicly)
	// Nginx auth_request calls GET /internal/route with X-Tenant-ID header
	// to determine the version group for upstream routing.
	tenantRouteHandler := handlers.NewTenantRouteHandler(cfg.TenantManager.GetRegistry())
	internal := router.Group("/internal")
	{
		internal.GET("/route", tenantRouteHandler.Route)
	}

	// Use provided factory registry — composition root must configure this
	factoryReg := cfg.Registry
	if factoryReg == nil {
		panic("v1.NewRouter: cfg.Registry must not be nil — use content.RegisterDefaults(reg) in main.go")
	}

	// Build metadata registry from factories (auto-registration)
	reg := metadata.NewRegistry()

	// API v1
	v1 := router.Group("/api/v1")
	{
		// Auth routes - need TenantDB middleware BEFORE auth
		registerAuthRoutes(v1, cfg, eventLogRepo)

		// Public system routes (no auth, no tenant needed)
		versionHandler := handlers.NewSystemVersionHandler(cfg.Version, cfg.BuildTime)
		v1.GET("/system/version", versionHandler.Version)

		// Protected endpoints - TenantDB runs first, then Auth
		protected := v1.Group("")
		protected.Use(middleware.TenantDB(cfg.TenantManager))      // 1. Resolve tenant, get DB pool
		protected.Use(middleware.Auth(cfg.JWTValidator))             // 2. Validate JWT
		protected.Use(middleware.RequireActiveTenant())              // 3. Block business requests for migration_failed
		// Security profiles are mandatory — fail-fast if misconfigured.
		if cfg.ProfileProvider == nil {
			panic("v1.NewRouter: cfg.ProfileProvider must not be nil — security profiles are required for DataScope")
		}
		protected.Use(middleware.SecurityContext(cfg.ProfileProvider))

		// Apply idempotency middleware for mutating operations
		if cfg.IdempotencyEnabled {
			protected.Use(idempotencyMiddleware(10 * time.Minute))
		}

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
		registerRegisterRoutes(protected, cfg, factoryReg)
		reportCompiler := registerReportRoutes(protected, cfg, factoryReg, reg)
		registerMetaRoutes(protected, reg, cfg.SchemaCache)
		registerRefResolverRoutes(protected, reg)
		registerUserPrefsRoutes(protected)
		registerListViewRoutes(protected)
		registerSettingsRoutes(protected)
		registerSecurityRoutes(protected, cfg)
		registerSystemRoutes(protected, eventLogRepo, cfg.SchemaCache, reg, cfg.WSTicketStore, reportCompiler)

		// Global data search (Ctrl+K) — available to all authenticated users.
		// Must be registered after entity routes so metadata.Registry is populated.
		searchService := search.NewService(reg)
		searchHandler := handlers.NewGlobalSearchHandler(searchService)
		protected.GET("/search", searchHandler.Search)

		// Entity preview (Command Palette → ArrowRight) — single entity preview card.
		previewHandler := handlers.NewEntityPreviewHandler(searchService)
		protected.GET("/search/preview", previewHandler.Preview)

		// Stateless XLSX renderer for document table parts (no entity binding needed).
		protected.POST("/export-table-part", handlers.ExportTablePart)
	}

	// Admin tenant management (Cloud Control Plane) — separate group with Auth,
	// but WITHOUT TenantDB middleware. These endpoints operate on meta-database only
	// and must remain accessible even when a tenant is in migration_failed status.
	adminAuthGroup := v1.Group("")
	adminAuthGroup.Use(middleware.TenantDB(cfg.TenantManager)) // still needed for X-Tenant-ID to resolve JWT
	adminAuthGroup.Use(middleware.Auth(cfg.JWTValidator))
	registerAdminTenantRoutes(adminAuthGroup, cfg, cfg.MigrationStateStore, healthHandler)

	// Internal endpoints for Updater Agent (shared secret — defense-in-depth beyond network isolation)
	registerInternalUpdaterRoutes(internal, cfg, cfg.MigrationStateStore)

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
func registerAuthRoutes(rg *gin.RouterGroup, cfg RouterConfig, eventWriter eventlog.Writer) {
	if cfg.AuthService == nil {
		return
	}

	baseHandler := handlers.NewBaseHandler()
	profileRepo := security_repo.NewProfileRepo()
	authHandler := handlers.NewAuthHandler(baseHandler, cfg.AuthService, profileRepo, eventWriter, cfg.WSTicketStore)

	// Public auth endpoints (no JWT required, but need tenant for DB access)
	// Rate limit login/register/refresh to prevent brute-force and credential stuffing.
	publicAuth := rg.Group("/auth")
	publicAuth.Use(middleware.TenantDB(cfg.TenantManager))
	publicAuth.Use(middleware.RateLimit(1, 5)) // 1 req/sec sustained, burst 5

	// Protected auth endpoints (JWT required)
	protectedAuth := rg.Group("/auth")
	protectedAuth.Use(middleware.TenantDB(cfg.TenantManager))
	protectedAuth.Use(middleware.Auth(cfg.JWTValidator))

	authHandler.RegisterRoutes(publicAuth, protectedAuth)
}

// registerCatalogRoutes registers catalog (reference) endpoints via the Abstract Factory registry.
// Also populates the metadata registry and builds refEndpoints map.
func registerCatalogRoutes(rg *gin.RouterGroup, cfg RouterConfig, factoryReg *FactoryRegistry, reg *metadata.Registry, eventWriter eventlog.Writer, currencyInvalidator domain.CurrencyCacheInvalidator) {
	catalogs := rg.Group("/catalog")

	deps := CatalogDeps{
		BaseHandler:              handlers.NewBaseHandler(),
		Numerator:                cfg.Numerator,
		PolicyEngine:             cfg.PolicyEngine,
		EventWriter:              eventWriter,
		CurrencyCacheInvalidator: currencyInvalidator,
	}

	// Build refEndpoints from factory declarations
	refEndpoints := map[string]string{
		"parent": "", // parent is self-referencing, skip
	}
	for _, factory := range factoryReg.Catalogs() {
		if rp, ok := factory.(platform.ReferenceProvider); ok {
			for _, refType := range rp.ReferenceTypes() {
				refEndpoints[refType] = "/catalog/" + factory.RoutePrefix()
			}
		}
	}

	// Iterate over registered catalog factories
	for _, factory := range factoryReg.Catalogs() {
		handler := factory.Build(deps)
		RegisterCatalogRoutes(catalogs.Group("/"+factory.RoutePrefix()), handler, factory.Permission())

		// Register reference mappings: refType → entityName (optional)
		if rp, ok := factory.(platform.ReferenceProvider); ok {
			for _, refType := range rp.ReferenceTypes() {
				reg.RegisterReferenceMapping(refType, factory.EntityName())
			}
		}

		// Auto-register metadata (optional: Inspectable, Presentable)
		var def metadata.EntityDef
		if insp, ok := factory.(platform.Inspectable); ok {
			def = metadata.Inspect(insp.EntityStruct(), factory.EntityName(), metadata.TypeCatalog)
		} else {
			def = metadata.EntityDef{Name: factory.EntityName(), Type: metadata.TypeCatalog}
		}
		if pres, ok := factory.(platform.Presentable); ok {
			def.Presentation = pres.EntityPresentation()
		}
		if tp, ok := factory.(platform.TableNameProvider); ok {
			def.TableName = tp.TableName()
		} else {
			// Derive table name from convention: cat_{routePrefix} (e.g. cat_counterparties)
			def.TableName = "cat_" + strings.ReplaceAll(factory.RoutePrefix(), "-", "_")
		}
		def.Key = deriveEntityKey(factory.Permission())
		def.RoutePrefix = factory.RoutePrefix()
		def.SetRefEndpoints(refEndpoints)
		if rls, ok := factory.(platform.RLSProvider); ok {
			def.RLSDimensions = rls.RLSDimensions()
		}
		if sf, ok := factory.(platform.SearchFieldsProvider); ok {
			fields := sf.SearchableFields()
			def.SearchColumns = &metadata.SearchColumns{
				SearchCols:  fields.SearchCols,
				TitleCol:    fields.TitleCol,
				SubtitleCol: fields.SubtitleCol,
			}
		}
		reg.Register(def)
	}
}

// registerDocumentRoutes registers document endpoints via the Abstract Factory registry.
// Each document type is wired by its DocumentRegistration (see document_factory.go).
// Also populates the metadata registry.
func registerDocumentRoutes(rg *gin.RouterGroup, cfg RouterConfig, factoryReg *FactoryRegistry, reg *metadata.Registry, eventWriter eventlog.Writer) {
	docsGroup := rg.Group("/document")

	stockRepo := register_repo.NewStockRepo()
	stockService := stock.NewService(stockRepo)
	costRepo := register_repo.NewCostRepo()
	costService := cost.NewService(costRepo)
	settlementRepo := register_repo.NewSettlementRepo()
	settlementService := settlement.NewService(settlementRepo)

	// Use injected PostingEngine or create default
	postingEngine := cfg.PostingEngine
	if postingEngine == nil {
		docLocker := postgres.NewDocLocker()
		recorders := posting.DefaultRecorders(stockService, costService, settlementService)
		postingEngine = posting.NewEngine(docLocker, recorders...)
	}

	// CurrencyResolver is guaranteed non-nil here — created in NewRouter before catalog/document registration.
	currencyResolver := cfg.CurrencyResolver

	printRegistry := printing.NewPrintFormRegistry()
	printRenderer, printErr := printing.NewRenderer()
	if printErr != nil {
		cfg.Logger.Errorw("failed to load print templates", "error", printErr)
	}

	deps := DocumentDeps{
		BaseHandler:      handlers.NewBaseHandler(),
		PostingEngine:    postingEngine,
		Numerator:        cfg.Numerator,
		CurrencyResolver: currencyResolver,
		PolicyEngine:     cfg.PolicyEngine,
		EventWriter:      eventWriter,
		OutboxPublisher:  postgres.NewOutboxPublisher(),
		PrintRegistry:    printRegistry,
		PrintRenderer:    printRenderer,
		RelatedDocFinder: postgres.NewRelatedDocRepo(reg),
		MovementProviders: []entity.MovementProvider{
			stockService,
			costService,
			settlementService,
		},
		MovementRefResolver: postgres.NewRefResolverRepo(reg),
		SettingsRepo:        postgres.NewSettingsRepo(),
		CurrencyMetadataResolver: cfg.CurrencyMetadataResolver,
	}

	// Build refEndpoints from catalog factories for document metadata
	refEndpoints := map[string]string{
		"parent": "",
	}
	for _, factory := range factoryReg.Catalogs() {
		if rp, ok := factory.(platform.ReferenceProvider); ok {
			for _, refType := range rp.ReferenceTypes() {
				refEndpoints[refType] = "/catalog/" + factory.RoutePrefix()
			}
		}
	}

	// Iterate over registered document factories
	for _, factory := range factoryReg.Documents() {
		handler := factory.Build(deps)
		RegisterDocumentRoutes(docsGroup.Group("/"+factory.RoutePrefix()), handler, factory.Permission())

		// Auto-register metadata (optional: Inspectable, Presentable)
		var def metadata.EntityDef
		if insp, ok := factory.(platform.Inspectable); ok {
			def = metadata.Inspect(insp.EntityStruct(), factory.EntityName(), metadata.TypeDocument)
		} else {
			def = metadata.EntityDef{Name: factory.EntityName(), Type: metadata.TypeDocument}
		}
		if pres, ok := factory.(platform.Presentable); ok {
			def.Presentation = pres.EntityPresentation()
		}
		def.Key = deriveEntityKey(factory.Permission())
		def.RoutePrefix = factory.RoutePrefix()
		if tp, ok := factory.(platform.TableNameProvider); ok {
			def.TableName = tp.TableName()
		} else {
			// Derive table name from convention: doc_{entityKey}s (e.g. doc_goods_receipts)
			def.TableName = "doc_" + def.Key + "s"
		}
		def.SetRefEndpoints(refEndpoints)
		if rls, ok := factory.(platform.RLSProvider); ok {
			def.RLSDimensions = rls.RLSDimensions()
		}
		if sf, ok := factory.(platform.SearchFieldsProvider); ok {
			fields := sf.SearchableFields()
			def.SearchColumns = &metadata.SearchColumns{
				SearchCols:  fields.SearchCols,
				TitleCol:    fields.TitleCol,
				SubtitleCol: fields.SubtitleCol,
			}
		}
		reg.Register(def)
	}
}

// registerRegisterRoutes registers accumulation register endpoints via the factory registry.
func registerRegisterRoutes(rg *gin.RouterGroup, cfg RouterConfig, factoryReg *FactoryRegistry) {
	registers := rg.Group("/registers")
	for _, reg := range factoryReg.Registers() {
		reg.RegisterRoutes(registers.Group("/"+reg.RoutePrefix()), cfg)
	}
}

// registerMetaRoutes registers metadata/schema endpoints.
func registerMetaRoutes(rg *gin.RouterGroup, reg *metadata.Registry, schemaCache *cache.SchemaCache) {
	handler := handlers.NewMetadataHandler(reg, schemaCache)
	meta := rg.Group("/meta")
	{
		meta.GET("", handler.ListEntities)
		meta.GET("/entities", handler.ListEntitiesSummary)
		meta.GET("/:name", handler.GetEntity)
		meta.GET("/:name/mock", handler.GetEntityMock)
		meta.GET("/:name/filters", handler.GetEntityFilters)
	}
}

// registerReportRoutes registers report endpoints via the factory registry.
// All reports use the Dataset-based Query Engine.
func registerReportRoutes(rg *gin.RouterGroup, cfg RouterConfig, factoryReg *FactoryRegistry, reg *metadata.Registry) *compiler.Compiler {
	reportsGroup := rg.Group("/reports")

	datasets := factoryReg.Datasets()
	if len(datasets) == 0 {
		return nil
	}

	baseHandler := handlers.NewBaseHandler()
	comp := compiler.NewCompiler(reg, datasets)
	dsHandler := handlers.NewDatasetReportHandler(baseHandler, comp, reg)

	variantRepo := postgres.NewReportVariantRepo()
	variantSvc := variants.NewService(variantRepo)
	variantHandler := handlers.NewReportVariantHandler(baseHandler, variantSvc)

	for _, ds := range datasets {
		group := reportsGroup.Group("/" + ds.Key)
		group.Use(middleware.RequirePermission(ds.Permission))
		{
			group.GET("/metadata", dsHandler.HandleMeta(ds.Key))
			group.POST("", dsHandler.HandleExecute)
			group.POST("/export", dsHandler.HandleExport(ds.Key))
			group.POST("/grouped", dsHandler.HandleGrouped(ds.Key))
			
			group.GET("/variants", variantHandler.GetList(ds.Key))
		}
	}

	reportsGroup.POST("/variants", variantHandler.Create)
	reportsGroup.PUT("/variants/:id", variantHandler.Update)
	reportsGroup.DELETE("/variants/:id", variantHandler.Delete)

	// Mount metadata under /metadata/reports/{key} for discoverability
	metaGroup := rg.Group("/metadata/reports")
	for _, ds := range datasets {
		metaGroup.GET("/"+ds.Key, dsHandler.HandleMeta(ds.Key))
	}

	return comp
}

// registerRefResolverRoutes registers the batch typed reference resolution endpoint.
// POST /api/v1/resolve-refs — resolves TypedRef (refType + refId) into presentations.
// Analogous to 1C's "ПолучитьПредставление()" for composite type fields.
func registerRefResolverRoutes(rg *gin.RouterGroup, reg *metadata.Registry) {
	resolver := postgres.NewRefResolverRepo(reg)
	handler := handlers.NewRefResolverHandler(resolver)
	rg.POST("/resolve-refs", handler.ResolveRefs)
}

// registerSecurityRoutes registers security profile and CEL policy rule management endpoints.
func registerSecurityRoutes(rg *gin.RouterGroup, cfg RouterConfig) {
	profileRepo := security_repo.NewProfileRepo()

	// Audit service (best-effort — handler works without it)
	auditSvc, _ := postgres.NewAuditService()

	profileHandler := handlers.NewSecurityProfileHandler(profileRepo, auditSvc, cfg.ProfileProvider)

	secGroup := rg.Group("/security")
	secGroup.Use(middleware.RequireRole("admin"))
	{
		// Security profile CRUD
		secGroup.GET("/profiles", profileHandler.List)
		secGroup.POST("/profiles", profileHandler.Create)
		secGroup.GET("/profiles/:profileId", profileHandler.Get)
		secGroup.PUT("/profiles/:profileId", profileHandler.Update)
		secGroup.DELETE("/profiles/:profileId", profileHandler.Delete)

		// User assignment to profiles
		secGroup.GET("/profiles/:profileId/users", profileHandler.ListProfileUsers)
		secGroup.POST("/profiles/:profileId/users", profileHandler.AssignUser)
		secGroup.DELETE("/profiles/:profileId/users/:userId", profileHandler.RemoveUser)

		// Audit history
		secGroup.GET("/profiles/:profileId/audit", profileHandler.GetAuditHistory)

		// CEL policy rules (require PolicyEngine)
		if cfg.PolicyEngine != nil {
			policyRuleRepo := security_repo.NewPolicyRuleRepo()
			policyRuleHandler := handlers.NewPolicyRuleHandler(policyRuleRepo, cfg.PolicyEngine, cfg.ProfileProvider)

			// CEL expression validation and testing (no profile context needed)
			secGroup.POST("/rules/validate", policyRuleHandler.ValidateExpression)
			secGroup.POST("/rules/test", policyRuleHandler.TestExpression)

			// Profile-scoped rule CRUD
			rulesGroup := secGroup.Group("/profiles/:profileId/rules")
			{
				rulesGroup.GET("", policyRuleHandler.List)
				rulesGroup.POST("", policyRuleHandler.Create)
				rulesGroup.GET("/:ruleId", policyRuleHandler.Get)
				rulesGroup.PUT("/:ruleId", policyRuleHandler.Update)
				rulesGroup.DELETE("/:ruleId", policyRuleHandler.Delete)
			}
		}
	}
}

// registerSystemRoutes registers system administration endpoints (event log, custom fields, processing).
func registerSystemRoutes(rg *gin.RouterGroup, eventLogReader eventlog.Reader, schemaCache *cache.SchemaCache, reg *metadata.Registry, wsTicketStore *auth.WSTicketStore, reportCompiler *compiler.Compiler) {
	sysGroup := rg.Group("/system")
	sysGroup.Use(middleware.RequireRole("admin"))

	eventLogHandler := handlers.NewEventLogHandler(eventLogReader)
	{
		sysGroup.GET("/event-log", eventLogHandler.List)
		sysGroup.GET("/event-log/stats", eventLogHandler.Stats)
		sysGroup.GET("/event-log/trace/:traceId", eventLogHandler.Trace)
		sysGroup.GET("/event-log/:id", eventLogHandler.Get)
	}

	// Custom field schema management (sys_custom_field_schemas)
	customFieldRepo := postgres.NewCustomFieldRepo()
	customFieldHandler := handlers.NewCustomFieldHandler(handlers.NewBaseHandler(), customFieldRepo, schemaCache)
	cfGroup := sysGroup.Group("/custom-fields")
	{
		cfGroup.GET("", customFieldHandler.List)
		cfGroup.POST("", customFieldHandler.Create)
		cfGroup.GET("/:id", customFieldHandler.Get)
		cfGroup.PUT("/:id", customFieldHandler.Update)
		cfGroup.DELETE("/:id", customFieldHandler.Delete)
	}

	// Notifications & Real-Time Hub
	notificationRepo := postgres.NewNotificationRepo()
	notifHandler := handlers.NewNotificationHandler(notificationRepo, wsTicketStore)
	
	// WebSockets — uses ticket-based auth, not JWT middleware
	rg.GET("/ws", notifHandler.ServeWS)

	// REST API for notifications (under /api/v1/system/notifications)
	notifUserGroup := rg.Group("/system/notifications")
	notifUserGroup.GET("", notifHandler.List)
	notifUserGroup.PUT("/mark-all-read", notifHandler.MarkAllAsRead) // static before /:id
	notifUserGroup.PUT("/:id/read", notifHandler.MarkAsRead)
	notifUserGroup.PUT("/:id/unread", notifHandler.MarkAsUnread)
	notifUserGroup.DELETE("/:id", notifHandler.Delete)

	// Processing: Find References (Найти ссылки на объект)
	refFinderRepo := postgres.NewRefFinderRepo(reg)
	refFinderHandler := handlers.NewRefFinderHandler(refFinderRepo)
	sysGroup.POST("/find-references", refFinderHandler.FindReferences)

	// Processing: Delete Marked Objects (Удаление помеченных объектов)
	markedRepo := postgres.NewMarkedObjectsRepo(reg)
	markedHandler := handlers.NewMarkedObjectsHandler(markedRepo)
	sysGroup.GET("/marked-objects", markedHandler.List)
	sysGroup.POST("/marked-objects/delete", markedHandler.Delete)

	// Admin Automations: Accounts (replaces old Service Accounts)
	automationAccountRepo := postgres.NewAutomationAccountRepo()
	automationAccountHandler := handlers.NewAutomationAccountHandler(handlers.NewBaseHandler(), automationAccountRepo, automationAccountRepo)
	automationAccountHandler.RegisterRoutes(sysGroup)

	// Admin Automations: Channels
	automationChannelRepo := postgres.NewAutomationChannelRepo()
	automationChannelHandler := handlers.NewAutomationChannelHandler(handlers.NewBaseHandler(), automationChannelRepo)
	automationChannelHandler.RegisterRoutes(sysGroup)

	// Admin Automations: Rules
	automationRuleRepo := postgres.NewAutomationRuleRepo()
	automationRuleHandler := handlers.NewAutomationRuleHandler(handlers.NewBaseHandler(), automationRuleRepo)
	automationRuleHandler.RegisterRoutes(sysGroup)

	// Admin Automations: History
	automationHistoryRepo := postgres.NewAutomationHistoryRepo()
	automationHistoryHandler := handlers.NewAutomationHistoryHandler(handlers.NewBaseHandler(), automationHistoryRepo)
	automationHistoryHandler.RegisterRoutes(sysGroup)

	// Admin Automations: Meta (enum values for UI)
	automationMetaHandler := handlers.NewAutomationMetaHandler(handlers.NewBaseHandler(), reportCompiler)
	automationMetaHandler.RegisterRoutes(sysGroup)
}

// deriveEntityKey extracts the snake_case entity key from a permission prefix.
// E.g. "catalog:counterparty" → "counterparty", "document:goods_receipt" → "goods_receipt".
func deriveEntityKey(permission string) string {
	parts := strings.SplitN(permission, ":", 2)
	if len(parts) >= 2 {
		return parts[1]
	}
	return permission
}

// registerUserPrefsRoutes registers user preferences endpoints.
func registerUserPrefsRoutes(rg *gin.RouterGroup) {
	baseHandler := handlers.NewBaseHandler()
	repo := auth_repo.NewUserPrefsRepo()
	handler := handlers.NewUserPrefsHandler(baseHandler, repo)
	handler.RegisterRoutes(rg)
}

// registerListViewRoutes registers saved list view (filter presets) endpoints.
func registerListViewRoutes(rg *gin.RouterGroup) {
	repo := postgres.NewListViewRepo()
	svc := listview.NewService(repo)
	handler := handlers.NewListViewHandler(handlers.NewBaseHandler(), svc)
	handlers.RegisterListViewRoutes(rg, handler)
}

// registerSettingsRoutes registers system settings endpoints.
func registerSettingsRoutes(rg *gin.RouterGroup) {
	baseHandler := handlers.NewBaseHandler()
	repo := postgres.NewSettingsRepo()
	handler := handlers.NewSettingsHandler(baseHandler, repo)
	handler.RegisterRoutes(rg)
}

// registerAdminTenantRoutes registers Cloud Control Plane endpoints.
// Admin-only: manage tenant version groups, schema versions, and migration recovery.
//
// IMPORTANT: This function is NOT called inside the `protected` group.
// Admin tenant endpoints operate on the meta-database, not tenant databases.
// They must remain accessible even when a tenant is in migration_failed status.
func registerAdminTenantRoutes(rg *gin.RouterGroup, cfg RouterConfig, stateStore tenant.MigrationStateStore, healthHandler *handlers.MultiTenantHealthHandler) {
	base := handlers.NewBaseHandler()
	registry := cfg.TenantManager.GetRegistry()
	updater := migration.NewTenantUpdater(registry, cfg.TenantManager, stateStore, cfg.Logger)
	h := handlers.NewAdminTenantHandler(base, registry, updater)

	admin := rg.Group("/admin/tenants")
	admin.Use(middleware.RequireRole("admin"))
	{
		admin.GET("", h.List)
		admin.GET("/stats", h.Stats)
		admin.GET("/:tenantId", h.Get)
		admin.PUT("/:tenantId/version-group", h.Promote)
		admin.PUT("/:tenantId/schema-version", h.UpdateSchemaVersion)
		admin.POST("/:tenantId/update", h.TriggerUpdate)
		admin.POST("/:tenantId/retry-update", h.RetryUpdate)
		admin.POST("/:tenantId/rollback-update", h.RollbackUpdate)
		admin.GET("/:tenantId/migration-status", h.MigrationStatus)
	}

	// Tenant health stats — admin-only (moved from public /health group)
	adminHealth := rg.Group("/admin")
	adminHealth.Use(middleware.RequireRole("admin"))
	adminHealth.GET("/health/tenants", healthHandler.TenantsStats)
}

// registerInternalUpdaterRoutes registers internal endpoints for the Updater Agent.
// No auth required — secured by Docker network isolation (internal network trust).
func registerInternalUpdaterRoutes(rg *gin.RouterGroup, cfg RouterConfig, stateStore tenant.MigrationStateStore) {
	// Shared secret middleware — defense-in-depth beyond Docker network isolation.
	// If INTERNAL_API_SECRET is set, require it in X-Internal-Secret header.
	if secret := os.Getenv("INTERNAL_API_SECRET"); secret != "" {
		rg.Use(func(c *gin.Context) {
			// SEC-03: constant-time comparison prevents timing side-channel attacks.
			if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Secret")), []byte(secret)) != 1 {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
		})
	}

	base := handlers.NewBaseHandler()
	registry := cfg.TenantManager.GetRegistry()
	updater := migration.NewTenantUpdater(registry, cfg.TenantManager, stateStore, cfg.Logger)
	h := handlers.NewAdminTenantHandler(base, registry, updater)

	rg.POST("/tenants/:id/trigger-update", h.InternalTriggerUpdate)
	rg.POST("/tenants/:id/retry-update", h.InternalRetryUpdate)
	rg.POST("/tenants/:id/rollback-update", h.InternalRollbackUpdate)
	rg.GET("/tenants/:id/migration-status", h.InternalMigrationStatus)
}

