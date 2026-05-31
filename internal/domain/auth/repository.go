// Package auth provides authentication and authorization domain logic.
package auth

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// UserRepository defines user storage operations.
type UserRepository interface {
	// Create creates a new user.
	Create(ctx context.Context, user *User) error

	// GetByID retrieves user by ID.
	GetByID(ctx context.Context, userID id.ID) (*User, error)

	// GetByEmail retrieves user by email (within tenant database).
	GetByEmail(ctx context.Context, email string) (*User, error)

	// Update updates user data.
	Update(ctx context.Context, user *User) error

	// Delete soft-deletes a user.
	Delete(ctx context.Context, userID id.ID) error

	// List retrieves users with filtering.
	List(ctx context.Context, filter UserFilter) ([]User, int, error)

	// LoadRoles loads user's roles.
	LoadRoles(ctx context.Context, userID id.ID) ([]Role, error)

	// LoadPermissions loads user's permissions (flattened from roles).
	LoadPermissions(ctx context.Context, userID id.ID) ([]string, error)

	// AssignRole assigns a role to user.
	AssignRole(ctx context.Context, userID, roleID id.ID, grantedBy id.ID) error

	// RevokeRole revokes a role from user.
	RevokeRole(ctx context.Context, userID, roleID id.ID) error

	// Exists checks if email exists (within tenant database).
	Exists(ctx context.Context, email string) (bool, error)
}

// RoleRepository defines role storage operations.
type RoleRepository interface {
	// Create creates a new role.
	Create(ctx context.Context, role *Role) error

	// GetByID retrieves role by ID.
	GetByID(ctx context.Context, roleID id.ID) (*Role, error)

	// GetByCode retrieves role by code (within tenant database).
	GetByCode(ctx context.Context, code string) (*Role, error)

	// Update updates role data.
	Update(ctx context.Context, role *Role) error

	// Delete deletes a role (only non-system roles).
	Delete(ctx context.Context, roleID id.ID) error

	// List retrieves roles (within tenant database).
	List(ctx context.Context) ([]Role, error)

	// LoadPermissions loads role's permissions.
	LoadPermissions(ctx context.Context, roleID id.ID) ([]Permission, error)

	// AssignPermission assigns a permission to role.
	AssignPermission(ctx context.Context, roleID, permissionID id.ID) error

	// RevokePermission revokes a permission from role.
	RevokePermission(ctx context.Context, roleID, permissionID id.ID) error

	// SetPermissions replaces all permissions for a role (delete + bulk insert).
	SetPermissions(ctx context.Context, roleID id.ID, permissionIDs []id.ID) error

	// CountUsersByRoleID returns the number of users assigned to a role.
	CountUsersByRoleID(ctx context.Context, roleID id.ID) (int, error)

	// ListUserIDsByRoleID returns user IDs assigned to a role.
	ListUserIDsByRoleID(ctx context.Context, roleID id.ID) ([]id.ID, error)
}

// PermissionRepository defines permission storage operations.
type PermissionRepository interface {
	// GetByCode retrieves permission by code.
	GetByCode(ctx context.Context, code string) (*Permission, error)

	// List retrieves all permissions.
	List(ctx context.Context) ([]Permission, error)

	// ListByResource retrieves permissions for a resource.
	ListByResource(ctx context.Context, resource string) ([]Permission, error)
}

// TokenRepository defines token storage operations.
type TokenRepository interface {
	// SaveRefreshToken saves a refresh token.
	SaveRefreshToken(ctx context.Context, token *RefreshToken) error

	// GetRefreshToken retrieves refresh token by hash.
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error)

	// RevokeRefreshToken revokes a refresh token.
	RevokeRefreshToken(ctx context.Context, tokenID id.ID, reason string) error

	// RevokeAllUserTokens revokes all tokens for a user.
	RevokeAllUserTokens(ctx context.Context, userID id.ID, reason string) error

	// CleanupExpiredTokens removes expired tokens.
	CleanupExpiredTokens(ctx context.Context) (int, error)
}

// AuthStateRepository defines server-side session and auth epoch operations.
type AuthStateRepository interface {
	// CreateSession creates a server-side auth session.
	CreateSession(ctx context.Context, session *AuthSession) error

	// GetSessionState retrieves the current server-side authority for a session.
	GetSessionState(ctx context.Context, userID, sessionID id.ID) (*AuthSessionState, error)

	// RevokeSession revokes a single auth session.
	RevokeSession(ctx context.Context, sessionID id.ID, reason string) error

	// RevokeAllUserSessions revokes all sessions for a user.
	RevokeAllUserSessions(ctx context.Context, userID id.ID, reason string) error

	// ExtendSession updates session expiry and last-seen metadata on refresh.
	ExtendSession(ctx context.Context, sessionID id.ID, expiresAt time.Time, info SessionInfo) error

	// BumpUserAuthVersion invalidates access tokens for one user.
	BumpUserAuthVersion(ctx context.Context, userID id.ID) (int64, error)

	// GetCurrentPolicyVersion returns the tenant-local RBAC policy epoch.
	GetCurrentPolicyVersion(ctx context.Context) (int64, error)

	// BumpPolicyVersion invalidates access tokens that carry the old RBAC policy epoch.
	BumpPolicyVersion(ctx context.Context) (int64, error)
}

// UserFilter for listing users.
type UserFilter struct {
	Search   string
	IsActive *bool
	RoleCode string
	Limit    int
	Offset   int
}
