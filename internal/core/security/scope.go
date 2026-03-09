// Package security provides authorization and access control.
package security

import (
	"context"
	"fmt"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/apperror"
)

// Permission defines available permissions in the system.
type Permission string

const (
	// CRUD permissions
	PermissionRead   Permission = "read"
	PermissionCreate Permission = "create"
	PermissionUpdate Permission = "update"
	PermissionDelete Permission = "delete"
	
	// Document-specific permissions
	PermissionPost   Permission = "post"
	PermissionUnpost Permission = "unpost"
	
	// Admin permissions
	PermissionAdmin  Permission = "admin"
	PermissionAudit  Permission = "audit"
)

// Role defines a set of permissions.
type Role string

const (
	RoleAdmin      Role = "admin"
	RoleAccountant Role = "accountant"
	RoleManager    Role = "manager"
	RoleViewer     Role = "viewer"
)

// AccessScope defines the boundaries of data visibility for current request.
// In Database-per-Tenant architecture this scope is used for authorization decisions
// (e.g. organization access) and for consistent logging/audit context.
type AccessScope struct {
	// TenantID is the current tenant (from request/JWT).
	TenantID string
	
	// UserID is the authenticated user
	UserID string
	
	// IsAdmin bypasses organization filtering
	IsAdmin bool
	
	// AllowedOrgIDs limits access to specific organizations
	// Empty = no access (unless IsAdmin)
	AllowedOrgIDs []string
	
	// Permissions available to user
	Permissions map[string][]Permission
}

// NewAccessScope creates AccessScope from context.
func NewAccessScope(ctx context.Context) *AccessScope {
	user := appctx.GetUser(ctx)
	if user == nil {
		return &AccessScope{}
	}
	
	return &AccessScope{
		TenantID:      user.TenantID,
		UserID:        user.UserID,
		IsAdmin:       user.IsAdmin,
		AllowedOrgIDs: user.OrgIDs,
	}
}

// CanAccessOrg checks if user can access organization.
func (s *AccessScope) CanAccessOrg(orgID string) bool {
	if s.IsAdmin {
		return true
	}
	for _, id := range s.AllowedOrgIDs {
		if id == orgID {
			return true
		}
	}
	return false
}

// HasPermission checks if user has permission on entity.
func (s *AccessScope) HasPermission(entity string, perm Permission) bool {
	if s.IsAdmin {
		return true
	}
	if perms, ok := s.Permissions[entity]; ok {
		for _, p := range perms {
			if p == perm {
				return true
			}
		}
	}
	return false
}

// RequirePermission returns error if permission is missing.
func (s *AccessScope) RequirePermission(entity string, perm Permission) error {
	if !s.HasPermission(entity, perm) {
		return apperror.NewForbidden(
			fmt.Sprintf("permission %s on %s required", perm, entity),
		).WithDetail("entity", entity).WithDetail("permission", perm)
	}
	return nil
}

// FilterOrgIDs returns intersection of requested and allowed org IDs.
// Used to safely filter queries by organization.
func (s *AccessScope) FilterOrgIDs(requestedOrgs []string) []string {
	if s.IsAdmin {
		return requestedOrgs
	}
	
	if len(requestedOrgs) == 0 {
		return s.AllowedOrgIDs
	}
	
	allowed := make(map[string]bool, len(s.AllowedOrgIDs))
	for _, id := range s.AllowedOrgIDs {
		allowed[id] = true
	}
	
	var result []string
	for _, id := range requestedOrgs {
		if allowed[id] {
			result = append(result, id)
		}
	}
	return result
}

// --- Context-based scope access ---

type scopeKey struct{}

// WithScope adds AccessScope to context.
func WithScope(ctx context.Context, scope *AccessScope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// GetScope returns AccessScope from context.
func GetScope(ctx context.Context) *AccessScope {
	if v, ok := ctx.Value(scopeKey{}).(*AccessScope); ok {
		return v
	}
	return NewAccessScope(ctx)
}
