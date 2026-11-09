// Package context provides request-scoped values extraction.
package context

import (
	"context"
	"slices"
	"time"
)

// UserContext contains authenticated user information.
type UserContext struct {
	UserID        string
	TenantID      string
	Email         string
	Roles         []string
	Permissions   []string
	IsAdmin       bool
	SessionID     string
	MFAVerifiedAt *time.Time
	MFAMethod     string
	MFAPending    bool
	MerchantIDs   []string       // UUID strings; empty = no portal access
	MerchantRoles map[string]int // merchant UUID string -> 1=Owner 2=Manager 3=Viewer
}

type userContextKey struct{}

// WithUser adds UserContext to context.
func WithUser(ctx context.Context, user *UserContext) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

// GetUser returns nil when no user has been attached to ctx.
func GetUser(ctx context.Context) *UserContext {
	if v, ok := ctx.Value(userContextKey{}).(*UserContext); ok {
		return v
	}
	return nil
}

// GetUserID returns user ID from context or empty string.
func GetUserID(ctx context.Context) string {
	if u := GetUser(ctx); u != nil {
		return u.UserID
	}
	return ""
}

// GetTenantID returns tenant ID from context or empty string.
func GetTenantID(ctx context.Context) string {
	if u := GetUser(ctx); u != nil {
		return u.TenantID
	}
	return ""
}

// HasRole checks if user has specific role.
func HasRole(ctx context.Context, role string) bool {
	u := GetUser(ctx)
	if u == nil {
		return false
	}
	return slices.Contains(u.Roles, role)
}

// IsAdminPrincipal reports whether the user belongs to the admin security boundary.
func IsAdminPrincipal(u *UserContext) bool {
	if u == nil {
		return false
	}
	return u.IsAdmin || slices.Contains(u.Roles, "admin") || slices.Contains(u.Roles, "security_admin")
}

// IsSystemAdmin reports whether the principal may use emergency business-data
// controls. security_admin is intentionally excluded: that role administers
// authentication/security, not accounting facts.
func IsSystemAdmin(u *UserContext) bool {
	if u == nil {
		return false
	}
	return u.IsAdmin || slices.Contains(u.Roles, "admin")
}
