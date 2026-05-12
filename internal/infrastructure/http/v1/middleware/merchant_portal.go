package middleware

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
)

// MerchantPortal extracts merchantIds from the JWT UserContext
// and injects a MerchantScope into the request context.
// Analogous to TenantDB middleware: a programming error to call
// portal endpoints without this middleware in the chain.
//
// Returns 403 Forbidden if the user has no merchant associations.
func MerchantPortal() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		if user == nil || len(user.MerchantIDs) == 0 {
			_ = c.Error(apperror.NewForbidden("portal access requires merchant association"))
			c.Abort()
			return
		}

		ids := make([]id.ID, 0, len(user.MerchantIDs))
		for _, s := range user.MerchantIDs {
			parsed, err := id.Parse(s)
			if err != nil {
				continue
			}
			ids = append(ids, parsed)
		}
		if len(ids) == 0 {
			_ = c.Error(apperror.NewForbidden("no valid merchant IDs in token"))
			c.Abort()
			return
		}

		scope := appctx.MerchantScope{
			MerchantIDs: ids,
			Role:        appctx.MerchantPortalRole(user.PortalRole),
		}
		ctx := appctx.WithMerchantScope(c.Request.Context(), scope)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequirePortalRole checks that the user's portal role meets the minimum level.
// Role values: 1=Owner < 2=Manager < 3=Viewer (lower = more privilege).
func RequirePortalRole(min appctx.MerchantPortalRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := appctx.GetMerchantScope(c.Request.Context())
		if !ok || scope.Role > min {
			_ = c.Error(apperror.NewForbidden("insufficient portal role"))
			c.Abort()
			return
		}
		c.Next()
	}
}
