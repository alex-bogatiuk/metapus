package merchant

import (
	"context"
	"slices"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// AccessScopeKind names merchant grants inside a session's access scopes.
// Grants map a merchant UUID to its PortalRole value.
const AccessScopeKind = "merchant"

// PortalRole defines access levels for portal users.
// iota+1: zero value = no access (Zero-value Safety invariant).
type PortalRole int

const (
	PortalRoleOwner   PortalRole = iota + 1 // full access + key management
	PortalRoleManager                       // operations without settings
	PortalRoleViewer                        // read-only
)

// IsValid reports whether role is a known portal role.
func (r PortalRole) IsValid() bool {
	return r >= PortalRoleOwner && r <= PortalRoleViewer
}

// PortalScope holds merchant IDs and per-merchant roles for the current
// request. Injected by MerchantPortal middleware.
//
// Architectural contract: every portal repository method MUST call
// MustGetPortalScope(ctx) and filter by scope.MerchantIDs.
type PortalScope struct {
	MerchantIDs []id.ID
	Roles       map[id.ID]PortalRole
}

type _portalScopeKey struct{}

// WithPortalScope injects PortalScope into context.
func WithPortalScope(ctx context.Context, scope PortalScope) context.Context {
	return context.WithValue(ctx, _portalScopeKey{}, scope)
}

// MustGetPortalScope extracts PortalScope from context.
// Panics if not present — this is a programming error (missing middleware),
// following the same pattern as TxManager.
func MustGetPortalScope(ctx context.Context) PortalScope {
	v, ok := ctx.Value(_portalScopeKey{}).(PortalScope)
	if !ok || len(v.MerchantIDs) == 0 {
		panic("merchant portal scope not injected — missing MerchantPortal middleware")
	}
	return v
}

// GetPortalScope extracts PortalScope from context if present.
func GetPortalScope(ctx context.Context) (PortalScope, bool) {
	v, ok := ctx.Value(_portalScopeKey{}).(PortalScope)
	return v, ok && len(v.MerchantIDs) > 0
}

// RoleFor returns the user's role for a specific merchant in the current scope.
func (s PortalScope) RoleFor(merchantID id.ID) (PortalRole, bool) {
	role, ok := s.Roles[merchantID]
	if !ok || !role.IsValid() || !slices.Contains(s.MerchantIDs, merchantID) {
		return 0, false
	}
	return role, true
}

// AllowsFor checks whether the user has at least min access for merchantID.
func (s PortalScope) AllowsFor(merchantID id.ID, min PortalRole) bool {
	role, ok := s.RoleFor(merchantID)
	return ok && role <= min
}

// PortalAccessScopes issues a user's merchant associations as session access
// scopes, so the platform auth service can embed them without knowing merchants.
type PortalAccessScopes struct {
	repo MerchantUserRepository
}

// NewPortalAccessScopes creates the merchant access scope provider.
func NewPortalAccessScopes(repo MerchantUserRepository) *PortalAccessScopes {
	return &PortalAccessScopes{repo: repo}
}

// AccessScopeKind returns the scope kind this provider fills.
func (*PortalAccessScopes) AccessScopeKind() string { return AccessScopeKind }

// AccessGrants returns merchant UUID -> PortalRole for the user.
func (p *PortalAccessScopes) AccessGrants(ctx context.Context, userID id.ID) (map[string]int, error) {
	assocs, err := p.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(assocs) == 0 {
		return nil, nil
	}
	grants := make(map[string]int, len(assocs))
	for _, a := range assocs {
		grants[a.MerchantID.String()] = int(a.Role)
	}
	return grants, nil
}
