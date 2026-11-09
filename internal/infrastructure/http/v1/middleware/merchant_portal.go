package middleware

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
)

// MerchantPortal extracts merchantIds and per-merchant roles from the JWT
// UserContext and injects a MerchantScope into the request context.
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
		if len(user.MerchantRoles) == 0 {
			_ = c.Error(apperror.NewTokenStale())
			c.Abort()
			return
		}

		ids := make([]id.ID, 0, len(user.MerchantIDs))
		roles := make(map[id.ID]appctx.MerchantPortalRole, len(user.MerchantIDs))
		for _, merchantID := range user.MerchantIDs {
			parsed, err := id.Parse(merchantID)
			if err != nil {
				continue
			}
			roleValue, ok := user.MerchantRoles[merchantID]
			if !ok {
				continue
			}
			role := appctx.MerchantPortalRole(roleValue)
			if !role.IsValid() {
				continue
			}
			ids = append(ids, parsed)
			roles[parsed] = role
		}
		if len(ids) == 0 {
			_ = c.Error(apperror.NewForbidden("no valid merchant access in token"))
			c.Abort()
			return
		}

		scope := appctx.MerchantScope{
			MerchantIDs: ids,
			Roles:       roles,
		}
		ctx := appctx.WithMerchantScope(c.Request.Context(), scope)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequirePortalRole checks that the user's role for the requested merchant
// meets the minimum level. Protected portal actions are merchant-bound: the
// request must carry an explicit merchant_id so authorization cannot degrade to
// "the user has this role somewhere".
// Role values: 1=Owner < 2=Manager < 3=Viewer (lower = more privilege).
func RequirePortalRole(min appctx.MerchantPortalRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := appctx.GetMerchantScope(c.Request.Context())
		if !ok {
			_ = c.Error(apperror.NewForbidden("insufficient portal role"))
			c.Abort()
			return
		}

		rawMerchantID := c.Query("merchant_id")
		if rawMerchantID == "" {
			_ = c.Error(apperror.NewValidation("merchant_id query parameter is required"))
			c.Abort()
			return
		}
		merchantID, err := id.Parse(rawMerchantID)
		if err != nil {
			_ = c.Error(apperror.NewValidation("invalid merchant_id"))
			c.Abort()
			return
		}
		if !scope.AllowsFor(merchantID, min) {
			_ = c.Error(apperror.NewForbidden("insufficient portal role"))
			c.Abort()
			return
		}
		c.Next()
	}
}
