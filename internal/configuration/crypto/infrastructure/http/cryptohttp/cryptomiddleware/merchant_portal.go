package cryptomiddleware

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/middleware"
)

// MerchantPortal extracts merchant grants and per-merchant roles from the
// JWT UserContext access scopes and injects a merchant.PortalScope into the
// request context. Analogous to TenantDB middleware: a programming error to
// call portal endpoints without this middleware in the chain.
//
// Returns 403 Forbidden if the user has no merchant associations.
func MerchantPortal() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		var grants map[string]int
		if user != nil {
			grants = user.AccessScopes.Grants(merchant.AccessScopeKind)
		}
		if len(grants) == 0 {
			_ = c.Error(apperror.NewForbidden("portal access requires merchant association"))
			c.Abort()
			return
		}

		ids := make([]id.ID, 0, len(grants))
		roles := make(map[id.ID]merchant.PortalRole, len(grants))
		for merchantID, roleValue := range grants {
			parsed, err := id.Parse(merchantID)
			if err != nil {
				continue
			}
			role := merchant.PortalRole(roleValue)
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
		slices.SortFunc(ids, func(a, b id.ID) int { return strings.Compare(a.String(), b.String()) })

		scope := merchant.PortalScope{
			MerchantIDs: ids,
			Roles:       roles,
		}
		ctx := merchant.WithPortalScope(c.Request.Context(), scope)
		ctx = middleware.WithCommandScope(ctx, portalCommandScope(scope))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// portalCommandScope scopes idempotent portal commands to the merchant the
// request names (merchant_id query or :merchantId), or to the only merchant
// in scope; any other merchant is rejected.
func portalCommandScope(scope merchant.PortalScope) middleware.CommandScopeResolver {
	return func(c *gin.Context) (string, string, error) {
		candidate := strings.TrimSpace(c.Query("merchant_id"))
		if candidate == "" {
			candidate = strings.TrimSpace(c.Param("merchantId"))
		}
		if candidate == "" && len(scope.MerchantIDs) == 1 {
			candidate = scope.MerchantIDs[0].String()
		}
		for _, merchantID := range scope.MerchantIDs {
			if merchantID.String() == candidate {
				return "merchant", candidate, nil
			}
		}
		return "", "", apperror.NewForbidden("merchant command scope is not allowed")
	}
}

// RequirePortalRole checks that the user's role for the requested merchant
// meets the minimum level. Protected portal actions are merchant-bound: the
// request must carry an explicit merchant_id so authorization cannot degrade to
// "the user has this role somewhere".
// Role values: 1=Owner < 2=Manager < 3=Viewer (lower = more privilege).
func RequirePortalRole(min merchant.PortalRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := merchant.GetPortalScope(c.Request.Context())
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
