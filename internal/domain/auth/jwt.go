// Package auth provides authentication and authorization domain logic.
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	appctx "metapus/internal/core/context"
)

// JWTConfig holds JWT configuration.
type JWTConfig struct {
	Secret          string
	Issuer          string
	AccessTokenTTL  time.Duration
}

// DefaultJWTConfig returns default JWT configuration.
func DefaultJWTConfig(secret string) JWTConfig {
	return JWTConfig{
		Secret:         secret,
		Issuer:         "metapus",
		AccessTokenTTL: 15 * time.Minute,
	}
}

// Claims represents JWT claims.
type Claims struct {
	jwt.RegisteredClaims
	UserID      string   `json:"uid"`
	TenantID    string   `json:"tid"`
	Email       string   `json:"email"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"perms,omitempty"`
	OrgIDs      []string `json:"orgs,omitempty"`
	IsAdmin     bool     `json:"adm,omitempty"`
}

// JWTService handles JWT operations.
type JWTService struct {
	config JWTConfig
}

// NewJWTService creates a new JWT service.
func NewJWTService(config JWTConfig) *JWTService {
	return &JWTService{config: config}
}

// GenerateAccessToken generates a new access token.
func (s *JWTService) GenerateAccessToken(
	userID, tenantID, email string,
	roles, permissions, orgIDs []string,
	isAdmin bool,
) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.config.AccessTokenTTL)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.config.Issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		UserID:      userID,
		TenantID:    tenantID,
		Email:       email,
		Roles:       roles,
		Permissions: permissions,
		OrgIDs:      orgIDs,
		IsAdmin:     isAdmin,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.config.Secret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}

	return tokenString, expiresAt, nil
}

// ValidateToken validates JWT and returns user context.
func (s *JWTService) ValidateToken(tokenString string) (*appctx.UserContext, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.Secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return &appctx.UserContext{
		UserID:   claims.UserID,
		TenantID: claims.TenantID,
		Email:    claims.Email,
		Roles:    claims.Roles,
		Permissions: claims.Permissions,
		OrgIDs:   claims.OrgIDs,
		IsAdmin:  claims.IsAdmin,
	}, nil
}
