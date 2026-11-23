package security_profile

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/cache"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

// ProfileProvider abstracts fetching a user's effective security profile.
// Middleware depends on this interface — not on concrete repo or cache types.
type ProfileProvider interface {
	// GetUserProfile returns the effective SecurityProfile for a user.
	// Returns nil (no error) if no profile is assigned.
	GetUserProfile(ctx context.Context, userID id.ID) (*SecurityProfile, error)

	// Invalidate removes a user's cached profile (e.g., after admin changes).
	Invalidate(userID id.ID)

	// InvalidateAll clears the entire cache.
	InvalidateAll()
}

// ─── Cached provider ─────────────────────────────────────────────────

type profileKey struct {
	tenantID string
	userID   id.ID
}

// CachedProfileProvider retains bounded, tenant-scoped profiles and coalesces loads.
type CachedProfileProvider struct {
	repo  Repository
	cache *cache.Loading[profileKey, *SecurityProfile]
}

func NewCachedProfileProvider(repo Repository, ttl time.Duration, maxEntries int) *CachedProfileProvider {
	return &CachedProfileProvider{repo: repo, cache: cache.NewLoading[profileKey, *SecurityProfile](ttl, maxEntries)}
}

func (p *CachedProfileProvider) GetUserProfile(ctx context.Context, userID id.ID) (*SecurityProfile, error) {
	key := profileKey{tenantID: tenant.GetTenantID(ctx), userID: userID}
	return p.cache.Get(ctx, key, func(ctx context.Context) (*SecurityProfile, error) {
		return p.repo.GetByUserID(ctx, userID)
	})
}

func (p *CachedProfileProvider) Invalidate(userID id.ID) {
	p.cache.Invalidate(func(key profileKey) bool { return key.userID == userID })
}

func (p *CachedProfileProvider) InvalidateAll() {
	p.cache.Invalidate(func(profileKey) bool { return true })
}

// ─── BuildSecurityContext is a convenience method ─────────────────────

// SecurityContext holds all security data for a request.
type SecurityContext struct {
	DataScope     *security.DataScope
	FieldPolicies map[string]*security.FieldPolicy
	PolicyRules   []security.PolicyRule
}

// BuildSecurityContext builds DataScope, FieldPolicies, and PolicyRules from a user's profile.
// Fail-open: nil profile (no profile assigned) = empty DataScope (no restrictions).
func BuildSecurityContext(profile *SecurityProfile, isAdmin bool) *SecurityContext {
	if isAdmin {
		return &SecurityContext{
			DataScope: &security.DataScope{IsAdmin: true},
		}
	}

	// No profile assigned — fail-open: empty DataScope = no restrictions
	if profile == nil {
		return &SecurityContext{
			DataScope: &security.DataScope{},
		}
	}

	scope := profile.BuildDataScope(false)

	// Convert domain PolicyRules to security.PolicyRule interface slice
	var rules []security.PolicyRule
	if len(profile.PolicyRules) > 0 {
		rules = make([]security.PolicyRule, len(profile.PolicyRules))
		for i, r := range profile.PolicyRules {
			rules[i] = r
		}
	}

	return &SecurityContext{
		DataScope:     scope,
		FieldPolicies: profile.FieldPolicies,
		PolicyRules:   rules,
	}
}

// Compile-time check.
var _ ProfileProvider = (*CachedProfileProvider)(nil)
