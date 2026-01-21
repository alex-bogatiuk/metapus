// Package auth provides authentication and authorization domain logic.
package auth

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// User represents a system user.
type User struct {
	ID                  id.ID      `db:"id" json:"id"`
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
	CreatedAt           time.Time  `db:"created_at" json:"createdAt"`
	UpdatedAt           time.Time  `db:"updated_at" json:"updatedAt"`
	DeletedAt           *time.Time `db:"deleted_at" json:"-"`
	Version             int        `db:"version" json:"version"`

	// Loaded relations
	Roles       []Role   `db:"-" json:"roles,omitempty"`
	Permissions []string `db:"-" json:"permissions,omitempty"`
	OrgIDs      []string `db:"-" json:"orgIds,omitempty"`
}

// NewUser creates a new user.
func NewUser(email, passwordHash string) *User {
	return &User{
		ID:           id.New(),
		Email:        email,
		PasswordHash: passwordHash,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Version:      1,
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

// HasPermission checks if user has a specific permission.
func (u *User) HasPermission(permissionCode string) bool {
	if u.IsAdmin {
		return true
	}
	for _, p := range u.Permissions {
		if p == permissionCode {
			return true
		}
	}
	return false
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
	ID          id.ID     `db:"id" json:"id"`
	Code        string    `db:"code" json:"code"`
	Name        string    `db:"name" json:"name"`
	Description string    `db:"description" json:"description,omitempty"`
	IsSystem    bool      `db:"is_system" json:"isSystem"`
	CreatedAt   time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt   time.Time `db:"updated_at" json:"updatedAt"`

	// Loaded relations
	Permissions []Permission `db:"-" json:"permissions,omitempty"`
}

// NewRole creates a new role.
func NewRole(code, name string) *Role {
	return &Role{
		ID:        id.New(),
		Code:      code,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// Permission represents a system permission.
type Permission struct {
	ID          id.ID     `db:"id" json:"id"`
	Code        string    `db:"code" json:"code"`
	Name        string    `db:"name" json:"name"`
	Description string    `db:"description" json:"description,omitempty"`
	Resource    string    `db:"resource" json:"resource"`
	Action      string    `db:"action" json:"action"`
	CreatedAt   time.Time `db:"created_at" json:"createdAt"`
}

// RefreshToken represents a refresh token for JWT refresh.
type RefreshToken struct {
	ID            id.ID      `db:"id"`
	UserID        id.ID      `db:"user_id"`
	TokenHash     string     `db:"token_hash"`
	ExpiresAt     time.Time  `db:"expires_at"`
	CreatedAt     time.Time  `db:"created_at"`
	RevokedAt     *time.Time `db:"revoked_at"`
	RevokedReason string     `db:"revoked_reason"`
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

// TokenPair contains access and refresh tokens.
type TokenPair struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	TokenType    string    `json:"tokenType"`
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
