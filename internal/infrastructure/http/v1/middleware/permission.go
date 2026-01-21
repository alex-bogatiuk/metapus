// Package middleware provides HTTP middleware for the API.
package middleware

import (
	"github.com/gin-gonic/gin"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/apperror"
)

// RequirePermission middleware checks if user has required permission.
// Admins automatically have all permissions.
func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		if user == nil {
			_ = c.Error(apperror.NewUnauthorized("authentication required"))
			c.Abort()
			return
		}

		// Admins have all permissions
		if user.IsAdmin {
			c.Next()
			return
		}

		// Check if user has the required permission
		// Permissions are loaded from JWT claims
		hasPermission := false
		for _, perm := range getUserPermissions(c) {
			if perm == permission {
				hasPermission = true
				break
			}
		}

		if !hasPermission {
			_ = c.Error(
				apperror.NewForbidden("insufficient permissions").
					WithDetail("required_permission", permission),
			)
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAnyPermission middleware checks if user has any of the required permissions.
func RequireAnyPermission(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		if user == nil {
			_ = c.Error(apperror.NewUnauthorized("authentication required"))
			c.Abort()
			return
		}

		// Admins have all permissions
		if user.IsAdmin {
			c.Next()
			return
		}

		// Check if user has any of the required permissions
		userPerms := getUserPermissions(c)
		for _, required := range permissions {
			for _, perm := range userPerms {
				if perm == required {
					c.Next()
					return
				}
			}
		}

		_ = c.Error(
			apperror.NewForbidden("insufficient permissions").
				WithDetail("required_permissions", permissions),
		)
		c.Abort()
	}
}

// RequireAllPermissions middleware checks if user has all required permissions.
func RequireAllPermissions(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		if user == nil {
			_ = c.Error(apperror.NewUnauthorized("authentication required"))
			c.Abort()
			return
		}

		// Admins have all permissions
		if user.IsAdmin {
			c.Next()
			return
		}

		// Check if user has all required permissions
		userPerms := getUserPermissions(c)
		userPermSet := make(map[string]bool)
		for _, p := range userPerms {
			userPermSet[p] = true
		}

		var missing []string
		for _, required := range permissions {
			if !userPermSet[required] {
				missing = append(missing, required)
			}
		}

		if len(missing) > 0 {
			_ = c.Error(
				apperror.NewForbidden("insufficient permissions").
					WithDetail("missing_permissions", missing),
			)
			c.Abort()
			return
		}

		c.Next()
	}
}

// getUserPermissions extracts permissions from context.
// Permissions are stored in the gin context by the Auth middleware.
func getUserPermissions(c *gin.Context) []string {
	if perms, exists := c.Get("permissions"); exists {
		if p, ok := perms.([]string); ok {
			return p
		}
	}
	return nil
}
