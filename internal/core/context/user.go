// Package context provides request-scoped values extraction.
package context

import (
	"context"
)

// UserContext contains authenticated user information.
type UserContext struct {
	UserID    string
	TenantID  string
	Email     string
	Roles     []string
	Permissions []string
	OrgIDs    []string // Organizations user has access to
	IsAdmin   bool
	SessionID string
}

type userContextKey struct{}

// WithUser adds UserContext to context.
func WithUser(ctx context.Context, user *UserContext) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

// GetUser returns UserContext from context.
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
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasOrgAccess checks if user has access to organization.
func HasOrgAccess(ctx context.Context, orgID string) bool {
	u := GetUser(ctx)
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	for _, id := range u.OrgIDs {
		if id == orgID {
			return true
		}
	}
	return false
}

// GetUserContext is an alias for GetUser for backwards compatibility.
func GetUserContext(ctx context.Context) *UserContext {
	return GetUser(ctx)
}
