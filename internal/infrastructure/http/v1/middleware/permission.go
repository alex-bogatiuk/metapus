// Package middleware provides HTTP middleware for the API.
package middleware

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/domain/eventlog"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

// permEventWriter is set via SetPermissionEventWriter during router init.
var permEventWriter DirectEventWriter

// SetPermissionEventWriter configures event logging for permission denials.
// Called once during router initialization.
func SetPermissionEventWriter(w DirectEventWriter) {
	permEventWriter = w
}

// emitPermissionDenied logs a security.permission_denied event (best-effort).
func emitPermissionDenied(c *gin.Context, user *appctx.UserContext, permission string) {
	if permEventWriter == nil {
		return
	}
	ctx := c.Request.Context()
	pool, err := tenantdb.GetPool(ctx)
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

// RequirePermissionOrSystemAdmin applies the explicit system-admin policy to
// routes that support it, including the admin role without an is_admin flag.
func RequirePermissionOrSystemAdmin(permission string) gin.HandlerFunc {
	requirePermission := RequirePermission(permission)
	return func(c *gin.Context) {
		if appctx.IsSystemAdmin(appctx.GetUser(c.Request.Context())) {
			c.Next()
			return
		}
		requirePermission(c)
	}
}

// CheckPermission verifies that the authenticated user holds permission (or is
// an admin). When denied, it records the security event, attaches the 403/401
// error to c, aborts the chain, and returns false.
func CheckPermission(c *gin.Context, permission string) bool {
	user := appctx.GetUser(c.Request.Context())
	if user == nil {
		_ = c.Error(apperror.NewUnauthorized("authentication required"))
		c.Abort()
		return false
	}

	// Admins have all permissions
	if user.IsAdmin {
		return true
	}

	// O(1) lookup via permissions_set built by Auth middleware
	if _, ok := getPermissionsSet(c)[permission]; !ok {
		emitPermissionDenied(c, user, permission)
		_ = c.Error(
			apperror.NewForbidden("insufficient permissions").
				WithDetail("required_permission", permission),
		)
		c.Abort()
		return false
	}

	return true
}

// RequirePermission middleware checks if user has required permission.
// Admins automatically have all permissions.
func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !CheckPermission(c, permission) {
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
