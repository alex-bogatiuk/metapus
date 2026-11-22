package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/pkg/logger"
)

const (
	// TenantHeader is the HTTP header for tenant identification.
	TenantHeader = "X-Tenant-ID"
)

// BindTenant puts tenant tenantID's database into ctx as the platform's
// tenant routes see it: the pool, a transaction manager over it and the
// tenant. The pool is not closed while it is bound; call release when the
// work on ctx is done. A route that finds its tenant itself (an API key, a
// public link) binds it with this instead of TenantDB.
func BindTenant(ctx context.Context, manager *tenantdb.Manager, tenantID string) (bound context.Context, release func(), err error) {
	managedPool, err := manager.GetPool(ctx, tenantID)
	if err != nil {
		return ctx, func() {}, err
	}
	managedPool.AcquireRef()
	bound = tenantdb.WithPool(ctx, managedPool.Pool())
	bound = tenant.WithTxManager(bound, postgres.NewTxManagerFromRawPool(managedPool.Pool()))
	bound = tenant.WithTenant(bound, managedPool.Tenant())
	return bound, managedPool.ReleaseRef, nil
}

// TenantDB middleware resolves tenant from header and injects database pool into context.
// This middleware MUST run before any database operations.
//
// Flow:
// 1. Extract tenant UUID from X-Tenant-ID header
// 2. Get pool from MultiTenantManager
// 3. Create TxManager for this request
// 4. Inject pool, TxManager, and Tenant into context
func TenantDB(manager *tenantdb.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 1. Extract tenant UUID from header
		rawTenantID := c.GetHeader(TenantHeader)
		if rawTenantID == "" {
			rawTenantID = c.Query("tenant")
		}
		if rawTenantID == "" {
			rawTenantID = c.Param("tenantId")
		}
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

		// 2. Bind the tenant's database to the request
		ctx, release, err := BindTenant(ctx, manager, tenantID)
		if err != nil {
			logger.Warn(ctx, "tenant pool error", "tenant_id", tenantID, "error", err)

			switch {
			case errors.Is(err, tenant.ErrTenantNotFound):
				_ = c.Error(apperror.NewNotFound("tenant", tenantID))
			case errors.Is(err, tenant.ErrTenantNotActive):
				_ = c.Error(apperror.NewForbidden("tenant is not active").WithDetail("tenant_id", tenantID))
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
		// The pool stays open while the request uses it (graceful shutdown).
		defer release()

		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
