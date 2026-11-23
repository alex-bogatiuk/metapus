package cryptomiddleware

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/checkout"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/middleware"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenantdb"
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

		ctx, release, err := middleware.BindTenant(c.Request.Context(), manager, tenantID)
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
		defer release()

		ctx = checkout.WithReference(ctx, ref)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
