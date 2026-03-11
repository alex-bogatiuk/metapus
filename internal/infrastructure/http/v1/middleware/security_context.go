package middleware

import (
	"github.com/gin-gonic/gin"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/security_profile"
	"metapus/pkg/logger"
)

// SecurityContext middleware builds DataScope and FieldPolicies from the
// authenticated user's security profile and injects them into request context.
//
// Must run AFTER Auth + UserContext middleware.
//
// Flow:
//  1. Extract UserContext from context (populated by Auth middleware).
//  2. If admin → DataScope{IsAdmin: true}, no FLS.
//  3. Load SecurityProfile via ProfileProvider (cached).
//  4. Build DataScope from JWT OrgIDs ∩ profile dimensions.
//  5. Inject DataScope + FieldPolicies into context.
func SecurityContext(provider security_profile.ProfileProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		user := appctx.GetUser(ctx)

		if user == nil {
			// No authenticated user — skip (Auth middleware handles 401)
			c.Next()
			return
		}

		// Admin bypass — full access, no FLS
		if user.IsAdmin {
			scope := &security.DataScope{IsAdmin: true}
			ctx = security.WithDataScope(ctx, scope)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// Parse user ID
		userID, err := id.Parse(user.UserID)
		if err != nil {
			logger.Warn(ctx, "security context: invalid user ID", "user_id", user.UserID, "error", err)
			// Fail-closed: build restrictive scope from JWT only
			sc := security_profile.BuildSecurityContext(nil, user.OrgIDs, false)
			ctx = security.WithDataScope(ctx, sc.DataScope)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// Load security profile (cached)
		profile, err := provider.GetUserProfile(ctx, userID)
		if err != nil {
			logger.Error(ctx, "security context: failed to load profile", "user_id", userID, "error", err)
			// Fail-closed: build restrictive scope from JWT only
			sc := security_profile.BuildSecurityContext(nil, user.OrgIDs, false)
			ctx = security.WithDataScope(ctx, sc.DataScope)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// Build DataScope + FieldPolicies + PolicyRules
		sc := security_profile.BuildSecurityContext(profile, user.OrgIDs, false)

		ctx = security.WithDataScope(ctx, sc.DataScope)
		ctx = security.WithFieldPolicies(ctx, sc.FieldPolicies)
		ctx = security.WithPolicyRules(ctx, sc.PolicyRules)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
