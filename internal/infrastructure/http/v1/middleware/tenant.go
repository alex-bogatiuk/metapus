package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

const (
	// TenantHeader is the HTTP header for tenant identification.
	TenantHeader = "X-Tenant-ID"
)

// TenantDB middleware resolves tenant from header and injects database pool into context.
// This middleware MUST run before any database operations.
//
// Flow:
// 1. Extract tenant UUID from X-Tenant-ID header
// 2. Get pool from MultiTenantManager
// 3. Create TxManager for this request
// 4. Inject pool, TxManager, and Tenant into context
func TenantDB(manager *tenant.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 1. Extract tenant UUID from header
		rawTenantID := c.GetHeader(TenantHeader)
		if rawTenantID == "" {
			_ = c.Error(
				apperror.NewValidation("tenant is required").
					WithDetail("header", TenantHeader),
			)
			c.Abort()
			return
		}

		tenantUUID, err := uuid.Parse(rawTenantID)
		if err != nil {
			_ = c.Error(
				apperror.NewValidation("invalid tenant id").
					WithDetail("header", TenantHeader).
					WithDetail("value", rawTenantID),
			)
			c.Abort()
			return
		}
		tenantID := tenantUUID.String()

		// 2. Get pool from manager
		managedPool, err := manager.GetPool(ctx, tenantID)
		if err != nil {
			logger.Warn(ctx, "tenant pool error", "tenant_id", tenantID, "error", err)

			switch {
			case errors.Is(err, tenant.ErrTenantNotFound):
				_ = c.Error(apperror.NewNotFound("tenant", tenantID))
			case errors.Is(err, tenant.ErrTenantNotActive):
				_ = c.Error(apperror.NewForbidden("tenant is not active").WithDetail("tenant_id", tenantID))
			case errors.Is(err, tenant.ErrTenantVersionMismatch):
				// 421 Misdirected Request: tenant belongs to a different version group.
				// The reverse proxy should route this request to the correct server instance.
				appErr := apperror.NewInternal(err)
				appErr.HTTPStatus = http.StatusMisdirectedRequest
				appErr.Message = "tenant version mismatch: request routed to wrong server instance"
				_ = c.Error(appErr.WithDetail("tenant_id", tenantID))
			case errors.Is(err, tenant.ErrMaxPoolLimit):
				appErr := apperror.NewInternal(err)
				appErr.HTTPStatus = http.StatusServiceUnavailable
				appErr.Message = "service temporarily unavailable"
				_ = c.Error(appErr.WithDetail("tenant_id", tenantID))
			default:
				_ = c.Error(apperror.NewInternal(err).WithDetail("tenant_id", tenantID))
			}
			c.Abort()
			return
		}

		// Track active request for graceful shutdown
		managedPool.AcquireRef()
		defer managedPool.ReleaseRef()

		// 3. Create TxManager for this request
		txManager := postgres.NewTxManagerFromRawPool(managedPool.Pool())

		// 4. Inject into context
		ctx = tenant.WithPool(ctx, managedPool.Pool())
		ctx = tenant.WithTxManager(ctx, txManager)
		ctx = tenant.WithTenant(ctx, managedPool.Tenant())

		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
