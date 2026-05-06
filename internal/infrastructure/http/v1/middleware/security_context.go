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
//  4. Build DataScope from profile dimensions (profile is sole source of org restrictions).
//  5. Run DimensionResolvers to dynamically add dimensions (e.g., merchant).
//  6. Inject DataScope + FieldPolicies into context.
//
// Fail-open: no profile assigned (nil, nil) → empty DataScope = no restrictions.
// Fail-closed: error loading profile → restrictive DataScope with no allowed values.
func SecurityContext(provider security_profile.ProfileProvider, resolvers ...security_profile.DimensionResolver) gin.HandlerFunc {
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
			// Fail-closed: restrictive scope (empty dimensions = no access)
			ctx = security.WithDataScope(ctx, &security.DataScope{
				Dimensions: map[string][]string{security.DimOrganization: {}},
			})
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// Load security profile (cached)
		profile, err := provider.GetUserProfile(ctx, userID)
		if err != nil {
			logger.Error(ctx, "security context: failed to load profile", "user_id", userID, "error", err)
			// Fail-closed: DB/cache error → restrictive scope (no access)
			ctx = security.WithDataScope(ctx, &security.DataScope{
				Dimensions: map[string][]string{security.DimOrganization: {}},
			})
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// profile == nil means no profile assigned → fail-open (full access)
		// profile != nil → build from profile dimensions
		sc := security_profile.BuildSecurityContext(profile, false)

		// Run dynamic dimension resolvers (e.g., merchant association)
		for _, resolver := range resolvers {
			ids, err := resolver.Resolve(ctx, userID)
			if err != nil {
				logger.Warn(ctx, "dimension resolver failed, skipping",
					"dimension", resolver.DimensionName(),
					"user_id", userID,
					"error", err,
				)
				continue
			}
			if ids != nil {
				// ids != nil means dimension applies → set in DataScope
				sc.DataScope.SetDimension(resolver.DimensionName(), ids)
			}
		}

		ctx = security.WithDataScope(ctx, sc.DataScope)
		ctx = security.WithFieldPolicies(ctx, sc.FieldPolicies)
		ctx = security.WithPolicyRules(ctx, sc.PolicyRules)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

