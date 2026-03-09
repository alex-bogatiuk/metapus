// Package middleware provides HTTP middleware for the Metapus API.
package middleware

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/security"
)

// UserContext extracts user ID from gin context and adds it to request context.
//
// This middleware must run AFTER Auth middleware, which sets "user_id" in gin context.
// The user ID is then available to domain layer via security.GetUserID(ctx).
//
// Usage in router:
//
//	protected.Use(middleware.Auth(cfg.JWTValidator))
//	protected.Use(middleware.UserContext())
func UserContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Auth middleware should have set user_id in gin context
		userID, exists := c.Get("user_id")
		if exists {
			if uid, ok := userID.(string); ok && uid != "" {
				// Add to request context for domain layer access
				ctx := security.WithUserID(c.Request.Context(), uid)
				c.Request = c.Request.WithContext(ctx)
			}
		}
		c.Next()
	}
}
