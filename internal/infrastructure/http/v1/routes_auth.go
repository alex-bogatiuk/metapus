package v1

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/security_repo"
)

// registerAuthRoutes registers authentication endpoints.
func registerAuthRoutes(rg *gin.RouterGroup, cfg RouterConfig, eventWriter *postgres.EventLogRepo) {
	if cfg.AuthSvc == nil {
		return
	}

	baseHandler := handlers.NewBaseHandler()
	profileRepo := security_repo.NewProfileRepo()
	authHandler := handlers.NewAuthHandler(baseHandler, cfg.AuthSvc, profileRepo, eventWriter, cfg.WSTicketStore)

	// Public auth endpoints (no JWT required, but need tenant for DB access)
	// Rate limit login/register/refresh to prevent brute-force and credential stuffing.
	publicAuth := rg.Group("/auth")
	publicAuth.Use(middleware.TenantDB(cfg.TenantManager))
	publicAuth.Use(middleware.RateLimit(1, 5)) // 1 req/sec sustained, burst 5

	// Protected auth endpoints (JWT required)
	protectedAuth := rg.Group("/auth")
	protectedAuth.Use(middleware.TenantDB(cfg.TenantManager))
	protectedAuth.Use(middleware.Auth(cfg.JWTValidator))
	protectedAuth.Use(middleware.AdminGuard(cfg.AdminSecurityMode, cfg.AdminAllowedCIDRs, cfg.AdminMFAMode, cfg.MFARepo, eventWriter))

	stepUp := func(actionCode string) gin.HandlerFunc {
		return middleware.SensitiveActionGuard(cfg.MFARepo, actionCode, eventWriter)
	}
	authHandler.RegisterRoutes(publicAuth, protectedAuth, stepUp)
}
