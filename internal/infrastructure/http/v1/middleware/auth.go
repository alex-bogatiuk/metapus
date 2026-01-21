package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	
	appctx "metapus/internal/core/context"
	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
)

// JWTValidator interface for token validation.
type JWTValidator interface {
	ValidateToken(tokenString string) (*appctx.UserContext, error)
}

// Auth middleware validates JWT tokens and populates user context.
func Auth(validator JWTValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			abortUnauthorized(c, "missing authorization header")
			return
		}
		
		// Check Bearer prefix
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			abortUnauthorized(c, "invalid authorization header format")
			return
		}
		
		tokenString := parts[1]
		
		// Validate token
		user, err := validator.ValidateToken(tokenString)
		if err != nil {
			_ = c.Error(apperror.NewUnauthorized("invalid token"))
			c.Abort()
			return
		}

		// Enforce tenant match: X-Tenant-ID resolved by TenantDB must match token tenant.
		resolvedTenantID := tenant.GetTenantID(c.Request.Context())
		if resolvedTenantID != "" && user.TenantID != "" && resolvedTenantID != user.TenantID {
			_ = c.Error(
				apperror.NewForbidden("tenant mismatch").
					WithDetail("header_tenant_id", resolvedTenantID).
					WithDetail("token_tenant_id", user.TenantID),
			)
			c.Abort()
			return
		}
		
		// Add user to context
		ctx := appctx.WithUser(c.Request.Context(), user)
		c.Request = c.Request.WithContext(ctx)
		
		// Store in gin context for easy access
		c.Set("user_id", user.UserID)
		c.Set("permissions", user.Permissions)
		
		c.Next()
	}
}

// OptionalAuth validates token if present, but doesn't require it.
func OptionalAuth(validator JWTValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}
		
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.Next()
			return
		}
		
		user, err := validator.ValidateToken(parts[1])
		if err == nil && user != nil {
			// Optional tenant mismatch check (only if TenantDB already resolved tenant)
			resolvedTenantID := tenant.GetTenantID(c.Request.Context())
			if resolvedTenantID != "" && user.TenantID != "" && resolvedTenantID != user.TenantID {
				// Ignore token if it belongs to another tenant
				c.Next()
				return
			}

			ctx := appctx.WithUser(c.Request.Context(), user)
			c.Request = c.Request.WithContext(ctx)
			c.Set("user_id", user.UserID)
			c.Set("permissions", user.Permissions)
		}
		
		c.Next()
	}
}

// RequireRole middleware checks if user has required role.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		if user == nil {
			_ = c.Error(apperror.NewUnauthorized("authentication required"))
			c.Abort()
			return
		}
		
		for _, required := range roles {
			for _, userRole := range user.Roles {
				if userRole == required {
					c.Next()
					return
				}
			}
		}
		_ = c.Error(
			apperror.NewForbidden("insufficient permissions").
				WithDetail("required_roles", roles),
		)
		c.Abort()
	}
}

func abortUnauthorized(c *gin.Context, message string) {
	_ = c.Error(apperror.NewUnauthorized(message))
	c.Abort()
}
