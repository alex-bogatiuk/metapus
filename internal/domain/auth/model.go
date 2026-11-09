// Package auth provides authentication and authorization domain logic.
package auth

import (
	"context"
	"slices"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// SessionInfo holds metadata for authentication sessions.
type SessionInfo struct {
	UserAgent string
	IPAddress string
}

// User represents a system user.
type User struct {
	entity.BaseEntity

	Email               string     `db:"email" json:"email"`
	PasswordHash        string     `db:"password_hash" json:"-"`
	FirstName           string     `db:"first_name" json:"firstName,omitempty"`
	LastName            string     `db:"last_name" json:"lastName,omitempty"`
	IsActive            bool       `db:"is_active" json:"isActive"`
	IsAdmin             bool       `db:"is_admin" json:"isAdmin"`
	EmailVerified       bool       `db:"email_verified" json:"emailVerified"`
	EmailVerifiedAt     *time.Time `db:"email_verified_at" json:"emailVerifiedAt,omitempty"`
	LastLoginAt         *time.Time `db:"last_login_at" json:"lastLoginAt,omitempty"`
	FailedLoginAttempts int        `db:"failed_login_attempts" json:"-"`
	LockedUntil         *time.Time `db:"locked_until" json:"-"`
	AuthVersion         int64      `db:"auth_version" json:"-"`

	// Loaded relations
	Roles       []Role   `db:"-" json:"roles,omitempty"`
	Permissions []string `db:"-" json:"permissions,omitempty"`

	// Portal claims — transient, populated by auth.Service during token generation.
	MerchantIDs []string `db:"-" json:"-"`

	// MFA status — transient, populated by auth.Service for API serialization.
	MFAEnabled     bool       `db:"-" json:"mfaEnabled"`
	MFAPending     bool       `db:"-" json:"mfaPending"`
	MFARequired    bool       `db:"-" json:"mfaRequired"`
	MFAGraceEndsAt *time.Time `db:"-" json:"mfaGraceEndsAt,omitempty"`
	MFAVerifiedAt  *time.Time `db:"-" json:"mfaVerifiedAt,omitempty"`
}

// NewUser creates a new user.
func NewUser(email, passwordHash string) *User {
	return &User{
		BaseEntity:   entity.NewBaseEntity(),
		Email:        email,
		PasswordHash: passwordHash,
		IsActive:     true,
	}
}

// Validate validates user data.
func (u *User) Validate(ctx context.Context) error {
	if u.Email == "" {
		return apperror.NewValidation("email is required").WithDetail("field", "email")
	}
	return nil
}

// IsLocked returns true if account is locked.
func (u *User) IsLocked() bool {
	if u.LockedUntil == nil {
		return false
	}
	return time.Now().Before(*u.LockedUntil)
}

// CanLogin checks if user can login.
func (u *User) CanLogin() error {
	if !u.IsActive {
		return apperror.NewForbidden("account is disabled")
	}
	if u.IsLocked() {
		return apperror.NewForbidden("account is temporarily locked")
	}
	// Require email verification for non-admin users.
	// Admin users and admin-created users bypass this check.
	if !u.EmailVerified && !u.IsAdmin {
		return apperror.NewForbidden("email not verified").
			WithDetail("email", u.Email).
			WithDetail("action", "verify_email")
	}
	return nil
}

// RecordFailedLogin increments failed login counter.
func (u *User) RecordFailedLogin(maxAttempts int, lockDuration time.Duration) {
	u.FailedLoginAttempts++
	if u.FailedLoginAttempts >= maxAttempts {
		lockUntil := time.Now().Add(lockDuration)
		u.LockedUntil = &lockUntil
	}
}

// RecordSuccessfulLogin resets failed login counter.
func (u *User) RecordSuccessfulLogin() {
	u.FailedLoginAttempts = 0
	u.LockedUntil = nil
	now := time.Now()
	u.LastLoginAt = &now
}

// HasRole checks if user has a specific role.
func (u *User) HasRole(roleCode string) bool {
	for _, r := range u.Roles {
		if r.Code == roleCode {
			return true
		}
	}
	return false
}

// IsAdminPrincipal reports whether this user belongs to the admin security boundary.
func (u *User) IsAdminPrincipal() bool {
	if u == nil {
		return false
	}
	return u.IsAdmin || u.HasRole("admin") || u.HasRole("security_admin")
}

// HasPermission checks if user has a specific permission.
func (u *User) HasPermission(permissionCode string) bool {
	if u.IsAdmin {
		return true
	}
	return slices.Contains(u.Permissions, permissionCode)
}

// FullName returns user's full name.
func (u *User) FullName() string {
	if u.FirstName == "" && u.LastName == "" {
		return u.Email
	}
	if u.LastName == "" {
		return u.FirstName
	}
	if u.FirstName == "" {
		return u.LastName
	}
	return u.FirstName + " " + u.LastName
}

// Role represents a user role.
type Role struct {
	entity.BaseEntity

	Code        string `db:"code" json:"code"`
	Name        string `db:"name" json:"name"`
	Description string `db:"description" json:"description,omitempty"`
	IsSystem    bool   `db:"is_system" json:"isSystem"`

	// Loaded relations
	Permissions []Permission `db:"-" json:"permissions,omitempty"`
}

// NewRole creates a new role.
func NewRole(code, name string) *Role {
	return &Role{
		BaseEntity: entity.NewBaseEntity(),
		Code:       code,
		Name:       name,
	}
}

// Permission represents a system permission.
type Permission struct {
	ID          id.ID  `db:"id" json:"id"`
	Code        string `db:"code" json:"code"`
	Name        string `db:"name" json:"name"`
	Description string `db:"description" json:"description,omitempty"`
	Resource    string `db:"resource" json:"resource"`
	Action      string `db:"action" json:"action"`
}

// RefreshToken represents a refresh token for JWT refresh.
type RefreshToken struct {
	ID            id.ID      `db:"id"`
	UserID        id.ID      `db:"user_id"`
	SessionID     id.ID      `db:"session_id"`
	TokenHash     string     `db:"token_hash"`
	ExpiresAt     time.Time  `db:"expires_at"`
	CreatedAt     time.Time  `db:"created_at"`
	RevokedAt     *time.Time `db:"revoked_at"`
	RevokedReason *string    `db:"revoked_reason"`
	UserAgent     string     `db:"user_agent"`
	IPAddress     string     `db:"ip_address"`
}

// IsValid checks if refresh token is valid.
func (t *RefreshToken) IsValid() bool {
	if t.RevokedAt != nil {
		return false
	}
	return time.Now().Before(t.ExpiresAt)
}

// AuthSession represents a server-side authentication session.
type AuthSession struct {
	ID                id.ID      `db:"id"`
	UserID            id.ID      `db:"user_id"`
	UserAuthVersion   int64      `db:"user_auth_version"`
	PolicyVersion     int64      `db:"policy_version"`
	CreatedAt         time.Time  `db:"created_at"`
	LastSeenAt        *time.Time `db:"last_seen_at"`
	ExpiresAt         time.Time  `db:"expires_at"`
	RevokedAt         *time.Time `db:"revoked_at"`
	RevokedReason     *string    `db:"revoked_reason"`
	UserAgent         string     `db:"user_agent"`
	IPAddress         string     `db:"ip_address"`
	MFAVerifiedAt     *time.Time `db:"mfa_verified_at"`
	MFAMethod         string     `db:"mfa_method"`
	MFAPending        bool       `db:"mfa_pending"`
	IdleExpiresAt     *time.Time `db:"idle_expires_at"`
	AbsoluteExpiresAt *time.Time `db:"absolute_expires_at"`
}

// AuthSessionState is the server-side authority used to validate access JWTs.
type AuthSessionState struct {
	SessionID         id.ID
	UserID            id.ID
	UserAuthVersion   int64
	PolicyVersion     int64
	UserActive        bool
	ExpiresAt         time.Time
	RevokedAt         *time.Time
	MFAVerifiedAt     *time.Time
	MFAMethod         string
	MFAPending        bool
	IdleExpiresAt     *time.Time
	AbsoluteExpiresAt *time.Time
}

// IsValid reports whether the session can authenticate an access token.
func (s *AuthSessionState) IsValid() bool {
	if s == nil || !s.UserActive || s.RevokedAt != nil {
		return false
	}
	now := time.Now()
	if !now.Before(s.ExpiresAt) {
		return false
	}
	if s.IdleExpiresAt != nil && !now.Before(*s.IdleExpiresAt) {
		return false
	}
	if s.AbsoluteExpiresAt != nil && !now.Before(*s.AbsoluteExpiresAt) {
		return false
	}
	return true
}

// TokenPair contains access and refresh tokens.
type TokenPair struct {
	CacheScope   CacheScope `json:"cacheScope"`
	AccessToken  string     `json:"accessToken"`
	RefreshToken string     `json:"refreshToken"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	TokenType    string     `json:"tokenType"`
}

// CacheScope identifies the data that a client may retain between token rotations.
// Versions are strings on the wire so long-lived counters remain exact in JavaScript.
type CacheScope struct {
	TenantID      string `json:"tenantId"`
	PrincipalID   string `json:"principalId"`
	SessionID     string `json:"sessionId"`
	UserVersion   string `json:"userVersion"`
	PolicyVersion string `json:"policyVersion"`
}

// AdminSecurityMode controls CIDR enforcement behavior.
type AdminSecurityMode string

const (
	AdminSecurityOff     AdminSecurityMode = "off"
	AdminSecurityWarn    AdminSecurityMode = "warn"
	AdminSecurityEnforce AdminSecurityMode = "enforce"
)

// MFAMode controls MFA rollout behavior for admin principals.
type MFAMode string

const (
	MFAModeOff      MFAMode = "off"
	MFAModeOptional MFAMode = "optional"
	MFAModeRequired MFAMode = "required"
)

// LoginStatus describes the next authentication step.
type LoginStatus string

const (
	LoginStatusComplete              LoginStatus = "complete"
	LoginStatusMFARequired           LoginStatus = "mfa_required"
	LoginStatusMFAEnrollmentRequired LoginStatus = "mfa_enrollment_required"
)

// LoginResult is returned by password login and MFA verification flows.
type LoginResult struct {
	Status             LoginStatus
	Tokens             *TokenPair
	User               *User
	ChallengeID        id.ID
	ChallengeExpiresAt time.Time
}

// MFAStatus is the non-secret enrollment state used by user views.
type MFAStatus struct {
	Enabled       bool
	RequiredAfter *time.Time
}

// TOTPRecord is the tenant-local MFA state for a user.
type TOTPRecord struct {
	UserID            id.ID
	SecretCiphertext  []byte
	SecretNonce       []byte
	Enabled           bool
	EnrolledAt        *time.Time
	DisabledAt        *time.Time
	RequiredAfter     *time.Time
	LastUsedTimeStep  *int64
	FailedAttempts    int
	FailedWindowStart *time.Time
	LockedUntil       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// MFAChallenge is a short-lived pre-authentication continuation token.
type MFAChallenge struct {
	ID                    id.ID
	UserID                id.ID
	ChallengeType         string
	SetupSecretCiphertext []byte
	SetupSecretNonce      []byte
	ExpiresAt             time.Time
	ConsumedAt            *time.Time
	CreatedAt             time.Time
	UserAgent             string
	IPAddress             string
}

// BackupCode is a hashed, one-time MFA recovery code.
type BackupCode struct {
	ID        id.ID
	UserID    id.ID
	CodeHash  string
	UsedAt    *time.Time
	CreatedAt time.Time
}

// MFAEnrollStartResult contains enrollment material returned once to the client.
type MFAEnrollStartResult struct {
	ChallengeID   id.ID
	OTPAuthURL    string
	QRCodeDataURL string
	ManualSecret  string
	ExpiresAt     time.Time
}

// MFAEnrollConfirmResult finalizes enrollment and returns one-time backup codes.
type MFAEnrollConfirmResult struct {
	LoginResult
	BackupCodes []string
}

// TokenSessionOptions controls server-side markers for a newly issued session.
type TokenSessionOptions struct {
	MFAVerifiedAt *time.Time
	MFAMethod     string
	MFAPending    bool
}

// Credentials for login.
type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterRequest for user registration.
type RegisterRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
}

// AssignRoleRequest for assigning role to user.
type AssignRoleRequest struct {
	UserID   id.ID  `json:"userId"`
	RoleCode string `json:"roleCode"`
}
