// Package middleware provides HTTP middleware for the API.
package middleware

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/eventlog"
	"metapus/internal/core/tenant"
	"metapus/pkg/logger"
)

// permEventWriter is set via SetPermissionEventWriter during router init.
var permEventWriter eventlog.DirectWriter

// SetPermissionEventWriter configures event logging for permission denials.
// Called once during router initialization.
func SetPermissionEventWriter(w eventlog.DirectWriter) {
	permEventWriter = w
}

// emitPermissionDenied logs a security.permission_denied event (best-effort).
func emitPermissionDenied(c *gin.Context, user *appctx.UserContext, permission string) {
	if permEventWriter == nil {
		return
	}
	ctx := c.Request.Context()
	pool, err := tenant.GetPool(ctx)
	if err != nil {
		return
	}
	if writeErr := permEventWriter.WriteDirect(ctx, pool, eventlog.Event{
		Category:  eventlog.CategorySecurity,
		Severity:  eventlog.SeverityWarning,
		EventType: eventlog.EventSecurityPermissionDenied,
		Source:    "api",
		UserID:    user.UserID,
		ClientIP:  c.ClientIP(),
		Message:   fmt.Sprintf("Permission denied: %s for user %s", permission, user.Email),
		Details: map[string]any{
			"required_permission": permission,
			"email":               user.Email,
			"path":                c.Request.URL.Path,
			"method":              c.Request.Method,
		},
	}); writeErr != nil {
		logger.Warn(ctx, "eventlog: failed to write permission denied event", "error", writeErr)
	}
}

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

		// O(1) lookup via permissions_set built by Auth middleware
		if _, ok := getPermissionsSet(c)[permission]; !ok {
			emitPermissionDenied(c, user, permission)
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

		// O(1) lookup via permissions_set built by Auth middleware
		permSet := getPermissionsSet(c)
		for _, required := range permissions {
			if _, ok := permSet[required]; ok {
				c.Next()
				return
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

		// O(1) lookup via permissions_set built by Auth middleware
		permSet := getPermissionsSet(c)
		var missing []string
		for _, required := range permissions {
			if _, ok := permSet[required]; !ok {
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

// getPermissionsSet returns the O(1) permission set built by Auth middleware.
// Returns an empty map if not available (fail-closed).
func getPermissionsSet(c *gin.Context) map[string]struct{} {
	if v, exists := c.Get("permissions_set"); exists {
		if ps, ok := v.(map[string]struct{}); ok {
			return ps
		}
	}
	return map[string]struct{}{}
}
