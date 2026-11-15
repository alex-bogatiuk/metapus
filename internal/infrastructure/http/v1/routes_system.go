package v1

import (
	"metapus/internal/application/integrationapp"
	"metapus/internal/core/tenant"
	integrationhttp "metapus/internal/infrastructure/integrations"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/domain/auth"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/listview"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/auth_repo"
	"metapus/internal/infrastructure/storage/postgres/security_repo"
	"metapus/internal/metadata"
)

// deriveEntityKey extracts the snake_case entity key from a permission prefix.
// E.g. "catalog:counterparty" → "counterparty", "document:crypto_invoice" → "crypto_invoice".
func deriveEntityKey(permission string) string {
	parts := strings.SplitN(permission, ":", 2)
	if len(parts) >= 2 {
		return parts[1]
	}
	return permission
}

// registerMetaRoutes registers metadata/schema endpoints.
func registerMetaRoutes(rg *gin.RouterGroup, reg *metadata.Registry) {
	handler := handlers.NewMetadataHandler(reg)
	meta := rg.Group("/meta")
	{
		meta.GET("", handler.ListEntities)
		meta.GET("/entities", handler.ListEntitiesSummary)
		meta.GET("/:name", handler.GetEntity)
		meta.GET("/:name/mock", handler.GetEntityMock)
		meta.GET("/:name/filters", handler.GetEntityFilters)
	}
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
	requireStepUp := func(actionCode string) gin.HandlerFunc {
		return middleware.SensitiveActionGuard(cfg.MFARepo, actionCode, cfg.EventLogRepo)
	}
	{
		// Security profile CRUD
		secGroup.GET("/profiles", profileHandler.List)
		secGroup.POST("/profiles", requireStepUp("security.profiles.create"), profileHandler.Create)
		secGroup.GET("/profiles/:profileId", profileHandler.Get)
		secGroup.PUT("/profiles/:profileId", requireStepUp("security.profiles.update"), profileHandler.Update)
		secGroup.DELETE("/profiles/:profileId", requireStepUp("security.profiles.delete"), profileHandler.Delete)

		// User assignment to profiles
		secGroup.GET("/profiles/:profileId/users", profileHandler.ListProfileUsers)
		secGroup.POST("/profiles/:profileId/users", requireStepUp("security.profiles.assign_user"), profileHandler.AssignUser)
		secGroup.DELETE("/profiles/:profileId/users/:userId", requireStepUp("security.profiles.remove_user"), profileHandler.RemoveUser)

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
				rulesGroup.POST("", requireStepUp("security.cel_rules.create"), policyRuleHandler.Create)
				rulesGroup.GET("/:ruleId", policyRuleHandler.Get)
				rulesGroup.PUT("/:ruleId", requireStepUp("security.cel_rules.update"), policyRuleHandler.Update)
				rulesGroup.DELETE("/:ruleId", requireStepUp("security.cel_rules.delete"), policyRuleHandler.Delete)
			}
		}
	}
}

// registerSystemRoutes registers system administration endpoints (event log, custom fields, processing).
// wsGroup is a separate group with TenantDB but without Auth middleware — used for ticket-based WebSocket auth.
func registerSystemRoutes(rg, sysGroup, wsGroup *gin.RouterGroup, cfg RouterConfig, eventLogReader eventlog.Reader, reg *metadata.Registry, wsTicketStore *auth.WSTicketStore, reportCompiler *compiler.Compiler) {
	requireStepUp := func(actionCode string) gin.HandlerFunc {
		return middleware.SensitiveActionGuard(cfg.MFARepo, actionCode, cfg.EventLogRepo)
	}

	eventLogHandler := handlers.NewEventLogHandler(eventLogReader)
	{
		sysGroup.GET("/event-log", eventLogHandler.List)
		sysGroup.GET("/event-log/stats", eventLogHandler.Stats)
		sysGroup.GET("/event-log/trace/:traceId", eventLogHandler.Trace)
		sysGroup.GET("/event-log/:id", eventLogHandler.Get)
	}

	// Notifications & Real-Time Hub
	notificationRepo := postgres.NewNotificationRepo()
	notifHandler := handlers.NewNotificationHandler(notificationRepo, wsTicketStore)

	// WebSockets — ticket-based auth, bypasses JWT middleware.
	// Registered on wsGroup (TenantDB only, no Auth) so the handler's
	// own ValidateTicket() is the sole authentication gate.
	wsGroup.GET("/ws", notifHandler.ServeWS)

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
	sysGroup.POST("/marked-objects/delete", requireStepUp("system.marked_objects.delete"), markedHandler.Delete)

	integrationRuntime := integration_repo.NewRuntime()
	handlers.NewIntegrationHandler(integrationapp.New(integrationRuntime, tenant.ContextTxManager{}, cfg.EventLogRepo), integrationapp.NewConnectionTestService(integrationRuntime, integrationhttp.ConnectionProber{Accounts: integrationRuntime, EndpointTransport: integration_repo.RPCTransport})).RegisterRoutes(sysGroup, requireStepUp("system.integrations.write"))

	// Admin Automations: Accounts (replaces old Service Accounts)
	automationAccountRepo := postgres.NewAutomationAccountRepo()
	automationAccountHandler := handlers.NewAutomationAccountHandler(handlers.NewBaseHandler(), automationAccountRepo, automationAccountRepo)
	automationAccountHandler.RegisterRoutes(sysGroup, requireStepUp("system.automation_accounts.write"))

	// Admin Automations: Channels
	automationChannelRepo := postgres.NewAutomationChannelRepo()
	automationChannelHandler := handlers.NewAutomationChannelHandler(handlers.NewBaseHandler(), automationChannelRepo)
	automationChannelHandler.RegisterRoutes(sysGroup, requireStepUp("system.automation_channels.write"))

	// Admin Automations: Rules
	automationRuleRepo := postgres.NewAutomationRuleRepo()
	automationRuleHandler := handlers.NewAutomationRuleHandler(handlers.NewBaseHandler(), automationRuleRepo)
	automationRuleHandler.RegisterRoutes(sysGroup, requireStepUp("system.automation_rules.write"))

	// Admin Automations: History
	automationHistoryRepo := postgres.NewAutomationHistoryRepo()
	automationHistoryHandler := handlers.NewAutomationHistoryHandler(handlers.NewBaseHandler(), automationHistoryRepo)
	automationHistoryHandler.RegisterRoutes(sysGroup)

	// Admin Automations: Meta (enum values for UI)
	automationMetaHandler := handlers.NewAutomationMetaHandler(handlers.NewBaseHandler(), reportCompiler)
	automationMetaHandler.RegisterRoutes(sysGroup)

	// Worker Job observability (/system/worker-jobs)
	workerJobRepo := postgres.NewWorkerJobReadRepo()
	workerJobHandler := handlers.NewWorkerJobHandler(workerJobRepo)
	workerJobHandler.RegisterRoutes(sysGroup)
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
func registerSettingsRoutes(rg *gin.RouterGroup, cfg RouterConfig) {
	baseHandler := handlers.NewBaseHandler()
	repo := postgres.NewSettingsRepo()
	handler := handlers.NewSettingsHandler(baseHandler, repo)
	handler.RegisterRoutes(rg, middleware.SensitiveActionGuard(cfg.MFARepo, "system.settings.update", cfg.EventLogRepo))
}

// registerAdminTenantRoutes registers read-only tenant monitoring endpoints.
func registerAdminTenantRoutes(rg *gin.RouterGroup, cfg RouterConfig, healthHandler *handlers.MultiTenantHealthHandler) {
	base := handlers.NewBaseHandler()
	registry := cfg.TenantManager.GetRegistry()
	h := handlers.NewAdminTenantHandler(base, registry)

	admin := rg.Group("/admin/tenants")
	admin.Use(middleware.RequireRole("admin"))
	{
		admin.GET("", h.List)
		admin.GET("/:tenantId", h.Get)
	}

	// Tenant health stats — admin-only (moved from public /health group)
	adminHealth := rg.Group("/admin")
	adminHealth.Use(middleware.RequireRole("admin"))
	adminHealth.GET("/health/tenants", healthHandler.TenantsStats)
}
