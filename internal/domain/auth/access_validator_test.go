package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

func TestAuthStateCacheInvalidationScopes(t *testing.T) {
	for _, scope := range []string{"session", "user", "policy"} {
		t.Run(scope, func(t *testing.T) {
			c := NewAuthStateCache(time.Minute, 10)
			user, other, session, secondSession := id.New(), id.New(), id.New(), id.New()
			type key struct {
				tenant        string
				user, session id.ID
			}
			keys := []key{{"a", user, session}, {"a", user, secondSession}, {"a", other, id.New()}, {"b", user, session}}
			calls := make(map[key]int)
			get := func(k key) {
				_, err := c.Get(context.Background(), k.tenant, k.user, k.session, func(context.Context) (*AuthSessionState, error) {
					calls[k]++
					return &AuthSessionState{UserID: k.user, SessionID: k.session}, nil
				})
				require.NoError(t, err)
			}
			for _, k := range keys {
				get(k)
			}
			switch scope {
			case "session":
				c.InvalidateSession("a", session)
			case "user":
				c.InvalidateUser("a", user)
			case "policy":
				c.InvalidatePolicy("a")
			}
			for _, k := range keys {
				get(k)
			}
			require.Equal(t, 2, calls[keys[0]])
			require.Equal(t, 1, calls[keys[3]], "other tenant must stay cached")
			if scope == "session" {
				require.Equal(t, 1, calls[keys[1]])
			} else {
				require.Equal(t, 2, calls[keys[1]])
			}
			if scope == "policy" {
				require.Equal(t, 2, calls[keys[2]])
			} else {
				require.Equal(t, 1, calls[keys[2]])
			}
		})
	}
}

type verifiedClaimsStub struct{ claims Claims }

func (s verifiedClaimsStub) ParseClaims(string) (*Claims, error) { return &s.claims, nil }

type unavailableSessionStore struct {
	AuthStateRepository
	err error
}

func (s unavailableSessionStore) GetSessionState(context.Context, id.ID, id.ID) (*AuthSessionState, error) {
	return nil, s.err
}

func TestSessionLookupFailureKeepsTechnicalCategory(t *testing.T) {
	claims := verifiedClaimsStub{Claims{UserID: id.New().String(), SessionID: id.New().String(), TenantID: "tenant-a"}}
	for _, tt := range []struct {
		err  error
		code string
	}{
		{errors.New("connection lost"), apperror.CodeInternal},
		{apperror.NewNotFound("session", "id"), apperror.CodeUnauthorized},
		{apperror.NewUnauthorized("invalid authority"), apperror.CodeUnauthorized},
	} {
		validator := NewAccessTokenValidator(claims, unavailableSessionStore{err: tt.err}, nil)
		_, err := validator.ValidateToken(t.Context(), "verified-token")
		appErr, ok := apperror.AsAppError(err)
		require.True(t, ok)
		require.Equal(t, tt.code, appErr.Code)
	}
}

func TestAuthStateCacheRejectsMismatchedIdentity(t *testing.T) {
	c := NewAuthStateCache(time.Minute, 10)
	user, session := id.New(), id.New()
	_, err := c.Get(context.Background(), "tenant", user, session, func(context.Context) (*AuthSessionState, error) {
		return &AuthSessionState{UserID: id.New(), SessionID: session}, nil
	})
	require.Error(t, err)
}

type activeSessionStore struct {
	AuthStateRepository
	state AuthSessionState
}

func (s activeSessionStore) GetSessionState(context.Context, id.ID, id.ID) (*AuthSessionState, error) {
	state := s.state
	return &state, nil
}

func TestValidateTokenRequiresCurrentClaimsFormat(t *testing.T) {
	userID, sessionID := id.New(), id.New()
	store := activeSessionStore{state: AuthSessionState{
		UserID: userID, SessionID: sessionID, UserAuthVersion: 3, PolicyVersion: 5,
		UserActive: true, ExpiresAt: time.Now().Add(time.Hour),
	}}
	base := Claims{UserID: userID.String(), SessionID: sessionID.String(), TenantID: "tenant-a", UserAuthVersion: 3, PolicyVersion: 5}

	legacy := base
	_, err := NewAccessTokenValidator(verifiedClaimsStub{legacy}, store, nil).ValidateToken(t.Context(), "legacy-token")
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeTokenStale, appErr.Code)

	current := base
	current.FormatVersion = ClaimsFormatVersion
	current.AccessScopes = appctx.AccessScopes{"merchant": {"m-1": 2}}
	user, err := NewAccessTokenValidator(verifiedClaimsStub{current}, store, nil).ValidateToken(t.Context(), "current-token")
	require.NoError(t, err)
	require.Equal(t, current.AccessScopes, user.AccessScopes)
}
