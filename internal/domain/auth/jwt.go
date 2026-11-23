package auth

import (
	"time"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
)

// ClaimsFormatVersion identifies the access token claim layout. Tokens with a
// different version are rejected as stale, so clients refresh them in place.
const ClaimsFormatVersion = 2

// Claims is the verified access identity, independent of the token format.
type Claims struct {
	UserID          string   `json:"uid"`
	TenantID        string   `json:"tid"`
	SessionID       string   `json:"sid"`
	UserAuthVersion int64    `json:"uv"`
	PolicyVersion   int64    `json:"pv"`
	Email           string   `json:"email"`
	Roles           []string `json:"roles"`
	Permissions     []string `json:"perms,omitempty"`
	IsAdmin         bool     `json:"adm,omitzero"`
	// AccessScopes carries configuration-defined object grants (kind -> ID -> level).
	AccessScopes  appctx.AccessScopes `json:"scp,omitempty"`
	FormatVersion int                 `json:"fv,omitempty"`
}

// AccessTokenIssuer creates signed access credentials.
type AccessTokenIssuer interface {
	GenerateAccessToken(userID, tenantID, sessionID, email string, userAuthVersion, policyVersion int64, roles, permissions []string, isAdmin bool, accessScopes appctx.AccessScopes) (string, time.Time, error)
}

// AccessTokenVerifier validates the cryptographic envelope before session checks.
type AccessTokenVerifier interface{ ParseClaims(string) (*Claims, error) }

// QRCodeEncoder renders an enrollment URI for presentation.
type QRCodeEncoder interface{ EncodeDataURL(string) (string, error) }
