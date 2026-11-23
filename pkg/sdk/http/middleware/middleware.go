// Package middleware is the platform's HTTP middleware: authentication,
// permissions, step-up, idempotency and errors.
package middleware

import (
	"context"
	"github.com/gin-gonic/gin"

	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
)

type (
	// CommandScopeResolver names the trusted owner that idempotent command keys
	// are scoped to, such as a configuration's partner account. Middleware that
	// authenticates such an owner installs it with WithCommandScope.
	CommandScopeResolver = core.CommandScopeResolver
	// StepUpChecker verifies fresh sensitive-action confirmations.
	StepUpChecker = core.StepUpChecker
)

const (
	HeaderIdempotencyKey = core.HeaderIdempotencyKey
	// TenantHeader is the HTTP header for tenant identification.
	TenantHeader = core.TenantHeader
)

// CommandIdempotency executes each mutating request as one durable application
// command. Authentication and merchant-scope middleware must run before it.
func CommandIdempotency(required bool) gin.HandlerFunc { return core.CommandIdempotency(required) }

// ErrorHandler middleware transforms errors into consistent JSON responses.
// Hides internal errors from clients while logging full details.
func ErrorHandler() gin.HandlerFunc { return core.ErrorHandler() }

// RequireAnyPermission middleware checks if user has any of the required permissions.
func RequireAnyPermission(permissions ...string) gin.HandlerFunc {
	return core.RequireAnyPermission(permissions...)
}

// RequirePermission middleware checks if user has required permission.
// Admins automatically have all permissions.
func RequirePermission(permission string) gin.HandlerFunc { return core.RequirePermission(permission) }

// RequirePermissionOrSystemAdmin applies the explicit system-admin policy to
// routes that support it, including the admin role without an is_admin flag.
func RequirePermissionOrSystemAdmin(permission string) gin.HandlerFunc {
	return core.RequirePermissionOrSystemAdmin(permission)
}

// RequireSystemAdmin fences emergency business-data actions. Keep this check
// in addition to RBAC so a security_admin cannot acquire emergency accounting
// controls merely by being inside the broader admin security boundary.
func RequireSystemAdmin() gin.HandlerFunc { return core.RequireSystemAdmin() }

// SensitiveActionGuard requires a fresh step-up confirmation for one action code.
func SensitiveActionGuard(checker core.StepUpChecker, actionCode string, writer core.DirectEventWriter) gin.HandlerFunc {
	return core.SensitiveActionGuard(checker, actionCode, writer)
}

// WithCommandScope makes idempotent commands in this request scope their
// keys through resolve instead of the authenticated user.
func WithCommandScope(ctx context.Context, resolve core.CommandScopeResolver) context.Context {
	return core.WithCommandScope(ctx, resolve)
}

// BindTenant puts tenant tenantID's database into ctx as the platform's
// tenant routes see it: the pool, a transaction manager over it and the
// tenant. The pool is not closed while it is bound; call release when the
// work on ctx is done. A route that finds its tenant itself (an API key, a
// public link) binds it with this instead of TenantDB.
func BindTenant(ctx context.Context, manager *tenantdb.Manager, tenantID string) (context.Context, func(), error) {
	return core.BindTenant(ctx, manager, tenantID)
}
