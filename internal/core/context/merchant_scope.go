package context

import (
	"context"

	"metapus/internal/core/id"
)

// MerchantPortalRole defines access levels for portal users.
// iota+1: zero value = no access (Zero-value Safety invariant).
type MerchantPortalRole int

const (
	PortalRoleOwner   MerchantPortalRole = iota + 1 // full access + key management
	PortalRoleManager                                // operations without settings
	PortalRoleViewer                                 // read-only
)

// MerchantScope holds the set of merchant IDs and the portal role
// for the current request. Injected by MerchantPortal middleware.
//
// Architectural contract: every portal repository method MUST call
// MustGetMerchantScope(ctx) and filter by scope.MerchantIDs.
type MerchantScope struct {
	MerchantIDs []id.ID
	Role        MerchantPortalRole
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
