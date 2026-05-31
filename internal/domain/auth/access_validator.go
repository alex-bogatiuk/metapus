package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
)

const defaultAuthStateCacheTTL = 5 * time.Minute

type authStateCacheEntry struct {
	state     *AuthSessionState
	expiresAt time.Time
}

// AuthStateCache is a single-instance, in-memory cache for session auth state.
// It removes the per-request database lookup from access-token validation while
// still allowing explicit invalidation on logout and permission changes.
type AuthStateCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]authStateCacheEntry
	sf      singleflight.Group
}

// NewAuthStateCache creates an in-memory auth state cache.
func NewAuthStateCache(ttl time.Duration) *AuthStateCache {
	if ttl <= 0 {
		ttl = defaultAuthStateCacheTTL
	}
	return &AuthStateCache{
		ttl:     ttl,
		entries: make(map[string]authStateCacheEntry),
	}
}

func authStateCacheKey(tenantID string, sessionID id.ID) string {
	return tenantID + ":" + sessionID.String()
}

func (c *AuthStateCache) Get(
	ctx context.Context,
	tenantID string,
	userID, sessionID id.ID,
	loader func(context.Context) (*AuthSessionState, error),
) (*AuthSessionState, error) {
	if c == nil {
		return loader(ctx)
	}

	key := authStateCacheKey(tenantID, sessionID)
	now := time.Now()

	c.mu.RLock()
	if entry, ok := c.entries[key]; ok && now.Before(entry.expiresAt) {
		c.mu.RUnlock()
		return entry.state, nil
	}
	c.mu.RUnlock()

	v, err, _ := c.sf.Do(key, func() (any, error) {
		c.mu.RLock()
		if entry, ok := c.entries[key]; ok && time.Now().Before(entry.expiresAt) {
			c.mu.RUnlock()
			return entry.state, nil
		}
		c.mu.RUnlock()

		state, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		if state.UserID != userID || state.SessionID != sessionID {
			return nil, apperror.NewUnauthorized("invalid session state")
		}

		c.mu.Lock()
		c.entries[key] = authStateCacheEntry{
			state:     state,
			expiresAt: time.Now().Add(c.ttl),
		}
		c.mu.Unlock()

		return state, nil
	})
	if err != nil {
		return nil, err
	}
	state, ok := v.(*AuthSessionState)
	if !ok {
		return nil, fmt.Errorf("invalid auth state cache value")
	}
	return state, nil
}

// InvalidateSession removes one cached session.
func (c *AuthStateCache) InvalidateSession(tenantID string, sessionID id.ID) {
	if c == nil || tenantID == "" || id.IsNil(sessionID) {
		return
	}
	c.mu.Lock()
	delete(c.entries, authStateCacheKey(tenantID, sessionID))
	c.mu.Unlock()
}

// InvalidateUser removes cached sessions for one user in one tenant.
func (c *AuthStateCache) InvalidateUser(tenantID string, userID id.ID) {
	if c == nil || tenantID == "" || id.IsNil(userID) {
		return
	}
	c.mu.Lock()
	for key, entry := range c.entries {
		if entry.state.UserID == userID && entry.state.SessionID != id.Nil() && len(key) > len(tenantID)+1 && key[:len(tenantID)+1] == tenantID+":" {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}

// InvalidatePolicy removes all cached sessions for a tenant.
func (c *AuthStateCache) InvalidatePolicy(tenantID string) {
	if c == nil || tenantID == "" {
		return
	}
	prefix := tenantID + ":"
	c.mu.Lock()
	for key := range c.entries {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}

// AccessTokenValidator validates access JWTs against server-side auth state.
type AccessTokenValidator struct {
	jwt   *JWTService
	repo  AuthStateRepository
	cache *AuthStateCache
}

// NewAccessTokenValidator creates a revocation-aware access-token validator.
func NewAccessTokenValidator(jwt *JWTService, repo AuthStateRepository, cache *AuthStateCache) *AccessTokenValidator {
	return &AccessTokenValidator{jwt: jwt, repo: repo, cache: cache}
}

// ValidateToken validates a JWT and verifies that its session and auth epochs
// are still current on the server.
func (v *AccessTokenValidator) ValidateToken(ctx context.Context, tokenString string) (*appctx.UserContext, error) {
	if v == nil || v.jwt == nil || v.repo == nil {
		return nil, apperror.NewUnauthorized("token validator is not configured")
	}

	claims, err := v.jwt.ParseClaims(tokenString)
	if err != nil {
		return nil, apperror.NewUnauthorized("invalid token").WithCause(err)
	}

	userID, err := id.Parse(claims.UserID)
	if err != nil {
		return nil, apperror.NewUnauthorized("invalid token user").WithCause(err)
	}
	sessionID, err := id.Parse(claims.SessionID)
	if err != nil || id.IsNil(sessionID) {
		return nil, apperror.NewUnauthorized("invalid token session").WithCause(err)
	}

	state, err := v.cache.Get(ctx, claims.TenantID, userID, sessionID, func(ctx context.Context) (*AuthSessionState, error) {
		return v.repo.GetSessionState(ctx, userID, sessionID)
	})
	if err != nil {
		return nil, apperror.NewUnauthorized("session not found").WithCause(err)
	}
	if !state.IsValid() {
		return nil, apperror.NewSessionRevoked()
	}
	if claims.UserAuthVersion != state.UserAuthVersion || claims.PolicyVersion != state.PolicyVersion {
		return nil, apperror.NewTokenStale()
	}

	return &appctx.UserContext{
		UserID:      claims.UserID,
		TenantID:    claims.TenantID,
		Email:       claims.Email,
		Roles:       claims.Roles,
		Permissions: claims.Permissions,
		IsAdmin:     claims.IsAdmin,
		SessionID:   claims.SessionID,
		MerchantIDs: claims.MerchantIDs,
		PortalRole:  claims.PortalRole,
	}, nil
}
