package security_profile

import (
	"context"
	"sync"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/security"
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

type cacheEntry struct {
	profile   *SecurityProfile
	expiresAt time.Time
}

// CachedProfileProvider wraps a Repository with an in-memory TTL cache.
type CachedProfileProvider struct {
	repo  Repository
	ttl   time.Duration
	mu    sync.RWMutex
	cache map[id.ID]*cacheEntry
}

// NewCachedProfileProvider creates a provider with in-memory caching.
// Default TTL is 5 minutes.
func NewCachedProfileProvider(repo Repository, ttl time.Duration) *CachedProfileProvider {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &CachedProfileProvider{
		repo:  repo,
		ttl:   ttl,
		cache: make(map[id.ID]*cacheEntry),
	}
}

func (p *CachedProfileProvider) GetUserProfile(ctx context.Context, userID id.ID) (*SecurityProfile, error) {
	// Check cache (read lock)
	p.mu.RLock()
	if entry, ok := p.cache[userID]; ok && time.Now().Before(entry.expiresAt) {
		p.mu.RUnlock()
		return entry.profile, nil
	}
	p.mu.RUnlock()

	// Cache miss — load from DB
	profile, err := p.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Store in cache (write lock)
	p.mu.Lock()
	p.cache[userID] = &cacheEntry{
		profile:   profile,
		expiresAt: time.Now().Add(p.ttl),
	}
	p.mu.Unlock()

	return profile, nil
}

func (p *CachedProfileProvider) Invalidate(userID id.ID) {
	p.mu.Lock()
	delete(p.cache, userID)
	p.mu.Unlock()
}

func (p *CachedProfileProvider) InvalidateAll() {
	p.mu.Lock()
	p.cache = make(map[id.ID]*cacheEntry)
	p.mu.Unlock()
}

// ─── BuildSecurityContext is a convenience method ─────────────────────

// SecurityContext holds all security data for a request.
type SecurityContext struct {
	DataScope     *security.DataScope
	FieldPolicies map[string]*security.FieldPolicy
	PolicyRules   []security.PolicyRule
}

// BuildSecurityContext builds DataScope, FieldPolicies, and PolicyRules from a user's profile.
// jwtOrgIDs come from the JWT token; isAdmin from the UserContext.
func BuildSecurityContext(profile *SecurityProfile, jwtOrgIDs []string, isAdmin bool) *SecurityContext {
	if isAdmin {
		return &SecurityContext{
			DataScope: &security.DataScope{IsAdmin: true},
		}
	}

	// No profile assigned — build DataScope from JWT orgs only, no FLS, no CEL rules
	if profile == nil {
		dims := map[string][]string{
			security.DimOrganization: jwtOrgIDs,
		}
		return &SecurityContext{
			DataScope: &security.DataScope{Dimensions: dims},
		}
	}

	scope := profile.BuildDataScope(jwtOrgIDs, false)

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
