// Package middleware is the platform's HTTP middleware: authentication,
// permissions, step-up, idempotency and errors.
package middleware

import (
	"context"
	"net/netip"

	"github.com/gin-gonic/gin"

	"metapus/internal/domain/auth"
	"metapus/internal/domain/security_profile"
	core "metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/tenantdb"
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

// AdminGuard applies admin CIDR and MFA controls to authenticated admin principals.
func AdminGuard(mode auth.AdminSecurityMode, allowedCIDRs []netip.Prefix, mfaMode auth.MFAMode, mfaRepo auth.MFARepository, writer core.DirectEventWriter) gin.HandlerFunc {
	return core.AdminGuard(mode, allowedCIDRs, mfaMode, mfaRepo, writer)
}

// Auth middleware validates JWT tokens and populates user context.
func Auth(validator core.JWTValidator) gin.HandlerFunc { return core.Auth(validator) }

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

// SecurityContext uses the shared application builder for HTTP and scheduled reads.
func SecurityContext(provider security_profile.ProfileProvider, resolvers ...security_profile.DimensionResolver) gin.HandlerFunc {
	return core.SecurityContext(provider, resolvers...)
}

// SensitiveActionGuard requires a fresh step-up confirmation for one action code.
func SensitiveActionGuard(checker core.StepUpChecker, actionCode string, writer core.DirectEventWriter) gin.HandlerFunc {
	return core.SensitiveActionGuard(checker, actionCode, writer)
}

// TenantDB middleware resolves tenant from header and injects database pool into context.
// This middleware MUST run before any database operations.
//
// Flow:
// 1. Extract tenant UUID from X-Tenant-ID header
// 2. Get pool from MultiTenantManager
// 3. Create TxManager for this request
// 4. Inject pool, TxManager, and Tenant into context
func TenantDB(manager *tenantdb.Manager) gin.HandlerFunc { return core.TenantDB(manager) }

// WithCommandScope makes idempotent commands in this request scope their
// keys through resolve instead of the authenticated user.
func WithCommandScope(ctx context.Context, resolve core.CommandScopeResolver) context.Context {
	return core.WithCommandScope(ctx, resolve)
}
