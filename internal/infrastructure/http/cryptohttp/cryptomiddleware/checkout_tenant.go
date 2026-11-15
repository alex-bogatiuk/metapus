package cryptomiddleware

import (
	"errors"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/checkout"
	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/pkg/logger"
)

// CheckoutTenantDB resolves tenant for public checkout routes from an opaque
// public checkout ref, custom domain, or dedicated deployment fallback.
func CheckoutTenantDB(manager *tenantdb.Manager, refs *checkout.Service) gin.HandlerFunc {
	if manager == nil {
		panic("CheckoutTenantDB middleware requires tenant manager")
	}
	if refs == nil {
		panic("CheckoutTenantDB middleware requires checkout reference service")
	}

	return func(c *gin.Context) {
		if c.GetHeader(middleware.TenantHeader) != "" || c.Query("tenant") != "" || c.Param("tenantId") != "" {
			_ = c.Error(apperror.NewNotFound("checkout", "reference"))
			c.Abort()
			return
		}

		publicRef := c.Param("publicRef")
		ref, tenantID, err := refs.ResolveTenant(c.Request.Context(), publicRef, c.Request.Host)
		if err != nil {
			_ = c.Error(apperror.NewNotFound("checkout", "reference"))
			c.Abort()
			return
		}

		managedPool, err := manager.GetPool(c.Request.Context(), tenantID)
		if err != nil {
			logger.Warn(c.Request.Context(), "checkout tenant pool error", "error", err)
			switch {
			case errors.Is(err, tenant.ErrTenantNotFound), errors.Is(err, tenant.ErrTenantNotActive):
				_ = c.Error(apperror.NewNotFound("checkout", "reference"))
			default:
				_ = c.Error(apperror.NewInternal(err))
			}
			c.Abort()
			return
		}

		managedPool.AcquireRef()
		defer managedPool.ReleaseRef()

		txManager := postgres.NewTxManagerFromRawPool(managedPool.Pool())
		ctx := tenantdb.WithPool(c.Request.Context(), managedPool.Pool())
		ctx = tenant.WithTxManager(ctx, txManager)
		ctx = tenant.WithTenant(ctx, managedPool.Tenant())
		ctx = checkout.WithReference(ctx, ref)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
