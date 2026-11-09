package context

import (
	"context"
	"slices"

	"metapus/internal/core/id"
)

// MerchantPortalRole defines access levels for portal users.
// iota+1: zero value = no access (Zero-value Safety invariant).
type MerchantPortalRole int

const (
	PortalRoleOwner   MerchantPortalRole = iota + 1 // full access + key management
	PortalRoleManager                               // operations without settings
	PortalRoleViewer                                // read-only
)

// MerchantScope holds merchant IDs and per-merchant roles for the current
// request. Injected by MerchantPortal middleware.
//
// Architectural contract: every portal repository method MUST call
// MustGetMerchantScope(ctx) and filter by scope.MerchantIDs.
type MerchantScope struct {
	MerchantIDs []id.ID
	Roles       map[id.ID]MerchantPortalRole
}

type _merchantScopeKey struct{}

// WithMerchantScope injects MerchantScope into context.
func WithMerchantScope(ctx context.Context, scope MerchantScope) context.Context {
	return context.WithValue(ctx, _merchantScopeKey{}, scope)
}

// MustGetMerchantScope extracts MerchantScope from context.
// Panics if not present — this is a programming error (missing middleware),
// following the same pattern as TxManager.
func MustGetMerchantScope(ctx context.Context) MerchantScope {
	v, ok := ctx.Value(_merchantScopeKey{}).(MerchantScope)
	if !ok || len(v.MerchantIDs) == 0 {
		panic("merchant_scope not injected — missing MerchantPortal middleware")
	}
	return v
}

// GetMerchantScope extracts MerchantScope from context if present.
func GetMerchantScope(ctx context.Context) (MerchantScope, bool) {
	v, ok := ctx.Value(_merchantScopeKey{}).(MerchantScope)
	return v, ok && len(v.MerchantIDs) > 0
}

// RoleFor returns the user's role for a specific merchant in the current scope.
func (s MerchantScope) RoleFor(merchantID id.ID) (MerchantPortalRole, bool) {
	role, ok := s.Roles[merchantID]
	if !ok || !role.IsValid() || !slices.Contains(s.MerchantIDs, merchantID) {
		return 0, false
	}
	return role, true
}

// AllowsFor checks whether the user has at least min access for merchantID.
func (s MerchantScope) AllowsFor(merchantID id.ID, min MerchantPortalRole) bool {
	role, ok := s.RoleFor(merchantID)
	return ok && role <= min
}

// IsValid reports whether role is a known portal role.
func (r MerchantPortalRole) IsValid() bool {
	return r >= PortalRoleOwner && r <= PortalRoleViewer
}
