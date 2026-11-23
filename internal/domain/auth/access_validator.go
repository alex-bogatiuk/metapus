package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/cache"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

type authStateKey struct {
	tenantID  string
	userID    id.ID
	sessionID id.ID
}

// AuthStateCache caches session authority for a single server instance.
// Keys include tenant and user so invalidation also fences pending loads.
type AuthStateCache struct {
	entries *cache.Loading[authStateKey, *AuthSessionState]
}

func NewAuthStateCache(ttl time.Duration, maxEntries int) *AuthStateCache {
	return &AuthStateCache{entries: cache.NewLoading[authStateKey, *AuthSessionState](ttl, maxEntries)}
}

func (c *AuthStateCache) Get(ctx context.Context, tenantID string, userID, sessionID id.ID, loader func(context.Context) (*AuthSessionState, error)) (*AuthSessionState, error) {
	load := func(ctx context.Context) (*AuthSessionState, error) {
		state, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		if state == nil || state.UserID != userID || state.SessionID != sessionID {
			return nil, apperror.NewUnauthorized("invalid session state")
		}
		return state, nil
	}
	if c == nil {
		return load(ctx)
	}
	return c.entries.Get(ctx, authStateKey{tenantID: tenantID, userID: userID, sessionID: sessionID}, load)
}

func (c *AuthStateCache) InvalidateSession(tenantID string, sessionID id.ID) {
	if c == nil || tenantID == "" || id.IsNil(sessionID) {
		return
	}
	c.entries.Invalidate(func(key authStateKey) bool { return key.tenantID == tenantID && key.sessionID == sessionID })
}

func (c *AuthStateCache) InvalidateUser(tenantID string, userID id.ID) {
	if c == nil || tenantID == "" || id.IsNil(userID) {
		return
	}
	c.entries.Invalidate(func(key authStateKey) bool { return key.tenantID == tenantID && key.userID == userID })
}

func (c *AuthStateCache) InvalidatePolicy(tenantID string) {
	if c == nil || tenantID == "" {
		return
	}
	c.entries.Invalidate(func(key authStateKey) bool { return key.tenantID == tenantID })
}

// AccessTokenValidator validates access JWTs against server-side auth state.
type AccessTokenValidator struct {
	jwt   AccessTokenVerifier
	repo  AuthStateRepository
	cache *AuthStateCache
}

// NewAccessTokenValidator creates a revocation-aware access-token validator.
func NewAccessTokenValidator(jwt AccessTokenVerifier, repo AuthStateRepository, cache *AuthStateCache) *AccessTokenValidator {
	return &AccessTokenValidator{jwt: jwt, repo: repo, cache: cache}
}

// ValidateToken validates a JWT and verifies that its session and auth epochs
// are still current on the server.
func (v *AccessTokenValidator) ValidateToken(ctx context.Context, tokenString string) (*appctx.UserContext, error) {
	if v == nil || v.jwt == nil || v.repo == nil {
		return nil, apperror.NewInternal(fmt.Errorf("token validator is not configured"))
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
		if apperror.IsNotFound(err) {
			return nil, apperror.NewUnauthorized("session not found").WithCause(err)
		}
		if appErr, ok := apperror.AsAppError(err); ok && appErr.Code == apperror.CodeUnauthorized {
			return nil, err
		}
		return nil, apperror.NewInternal(err)
	}
	if !state.IsValid() {
		return nil, apperror.NewSessionRevoked()
	}
	if claims.UserAuthVersion != state.UserAuthVersion || claims.PolicyVersion != state.PolicyVersion {
		return nil, apperror.NewTokenStale()
	}
	if claims.FormatVersion != ClaimsFormatVersion {
		return nil, apperror.NewTokenStale()
	}

	return &appctx.UserContext{
		UserID:        claims.UserID,
		TenantID:      claims.TenantID,
		Email:         claims.Email,
		Roles:         claims.Roles,
		Permissions:   claims.Permissions,
		IsAdmin:       claims.IsAdmin,
		SessionID:     claims.SessionID,
		MFAVerifiedAt: state.MFAVerifiedAt,
		MFAMethod:     state.MFAMethod,
		MFAPending:    state.MFAPending,
		AccessScopes:  claims.AccessScopes,
	}, nil
}
