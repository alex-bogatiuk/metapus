package middleware

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
)

// RequireActiveTenant blocks business requests when the tenant is not fully active.
// This should be applied AFTER TenantDB middleware (which resolves the tenant).
//
// Tenants in migration_failed or updating status can still authenticate and
// access admin endpoints, but cannot perform business operations.
func RequireActiveTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		t := tenant.GetTenant(c.Request.Context())
		if t == nil {
			// TenantDB middleware didn't run or failed — let it handle the error.
			c.Next()
			return
		}

		if !t.CanAcceptBusinessRequests() {
			_ = c.Error(
				apperror.NewForbidden("tenant is under maintenance").
					WithDetail("tenant_id", t.ID).
					WithDetail("status", string(t.Status)),
			)
			c.Abort()
			return
		}

		c.Next()
	}
}
