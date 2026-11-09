package auth

import "time"

// Claims is the verified access identity, independent of the token format.
type Claims struct {
	UserID          string         `json:"uid"`
	TenantID        string         `json:"tid"`
	SessionID       string         `json:"sid"`
	UserAuthVersion int64          `json:"uv"`
	PolicyVersion   int64          `json:"pv"`
	Email           string         `json:"email"`
	Roles           []string       `json:"roles"`
	Permissions     []string       `json:"perms,omitempty"`
	IsAdmin         bool           `json:"adm,omitzero"`
	MerchantIDs     []string       `json:"mids,omitempty"`
	MerchantRoles   map[string]int `json:"mrs,omitempty"`
}

// AccessTokenIssuer creates signed access credentials.
type AccessTokenIssuer interface {
	GenerateAccessToken(userID, tenantID, sessionID, email string, userAuthVersion, policyVersion int64, roles, permissions []string, isAdmin bool, merchantIDs []string, merchantRoles map[string]int) (string, time.Time, error)
}

// AccessTokenVerifier validates the cryptographic envelope before session checks.
type AccessTokenVerifier interface{ ParseClaims(string) (*Claims, error) }

// QRCodeEncoder renders an enrollment URI for presentation.
type QRCodeEncoder interface{ EncodeDataURL(string) (string, error) }
