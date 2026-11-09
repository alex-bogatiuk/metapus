package authjwt

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"metapus/internal/domain/auth"
)

func TestJWTServiceRejectsDifferentHMACAlgorithm(t *testing.T) {
	t.Parallel()
	const secret = "test-secret-at-least-32-bytes-long"
	service := NewJWTService(DefaultJWTConfig(secret))
	claims := wireClaims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    "metapus",
		Subject:   "user-1",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	raw, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	_, err = service.ParseClaims(raw)
	require.ErrorContains(t, err, "unexpected signing method")
}

func TestAccessTokenRoundTripPreservesIdentityAndRevocationClaims(t *testing.T) {
	service := NewJWTService(DefaultJWTConfig("test-secret-at-least-32-bytes-long"))
	roles := map[string]int{"merchant-1": 2}
	token, expires, err := service.GenerateAccessToken("user-1", "tenant-a", "session-1", "u@example.test", 7, 11, []string{"owner"}, []string{"invoice:read"}, true, []string{"merchant-1"}, roles)
	require.NoError(t, err)
	require.True(t, expires.After(time.Now()))
	claims, err := service.ParseClaims(token)
	require.NoError(t, err)
	require.Equal(t, &auth.Claims{UserID: "user-1", TenantID: "tenant-a", SessionID: "session-1", Email: "u@example.test", UserAuthVersion: 7, PolicyVersion: 11, Roles: []string{"owner"}, Permissions: []string{"invoice:read"}, IsAdmin: true, MerchantIDs: []string{"merchant-1"}, MerchantRoles: roles}, claims)
	other := NewJWTService(JWTConfig{Secret: service.config.Secret, Issuer: "different"})
	_, err = other.ParseClaims(token)
	require.Error(t, err)
}
