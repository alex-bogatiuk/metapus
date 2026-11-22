// Package appctx carries the request's user, tenant and access scopes in a
// context.
package appctx

import (
	"context"

	core "metapus/internal/core/context"
)

type (
	// AccessScopes holds object-level grants that a configuration attaches to a
	// session: scope kind -> object ID -> access level. The platform carries them
	// opaquely; the configuration that owns a kind defines what its levels mean.
	AccessScopes = core.AccessScopes
	// UserContext contains authenticated user information.
	UserContext = core.UserContext
)

// GetTenantID returns tenant ID from context or empty string.
func GetTenantID(ctx context.Context) string { return core.GetTenantID(ctx) }

// GetUser returns nil when no user has been attached to ctx.
func GetUser(ctx context.Context) *core.UserContext { return core.GetUser(ctx) }

// GetUserID returns user ID from context or empty string.
func GetUserID(ctx context.Context) string { return core.GetUserID(ctx) }

// IsSystemAdmin reports whether the principal may use emergency business-data
// controls. security_admin is intentionally excluded: that role administers
// authentication/security, not accounting facts.
func IsSystemAdmin(u *core.UserContext) bool { return core.IsSystemAdmin(u) }

// WithUser adds UserContext to context.
func WithUser(ctx context.Context, user *core.UserContext) context.Context {
	return core.WithUser(ctx, user)
}
