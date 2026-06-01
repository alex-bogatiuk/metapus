// Package auth provides authentication and authorization domain logic.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/pkg/logger"
)

// BcryptCost is the bcrypt work factor for password hashing.
// OWASP recommends >= 12. Higher values increase brute-force resistance
// at the cost of ~150ms additional latency per login.
const BcryptCost = 12

// ServiceConfig holds auth service configuration.
type ServiceConfig struct {
	MaxLoginAttempts   int
	LockDuration       time.Duration
	PasswordMinLength  int
	RefreshTokenExpiry time.Duration
}

// DefaultServiceConfig returns default configuration.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		MaxLoginAttempts:   5,
		LockDuration:       15 * time.Minute,
		PasswordMinLength:  8,
		RefreshTokenExpiry: 7 * 24 * time.Hour, // 7 days
	}
}

// Service provides authentication and authorization logic.
type Service struct {
	userRepo         UserRepository
	roleRepo         RoleRepository
	permRepo         PermissionRepository
	tokenRepo        TokenRepository
	authStateRepo    AuthStateRepository
	authStateCache   *AuthStateCache
	merchantUserRepo merchant.MerchantUserRepository
	txManager        tx.Manager
	jwtService       *JWTService
	config           ServiceConfig
}

// NewService creates a new auth service.
func NewService(
	userRepo UserRepository,
	roleRepo RoleRepository,
	permRepo PermissionRepository,
	tokenRepo TokenRepository,
	authStateRepo AuthStateRepository,
	authStateCache *AuthStateCache,
	merchantUserRepo merchant.MerchantUserRepository,
	txManager tx.Manager,
	jwtService *JWTService,
	config ServiceConfig,
) *Service {
	return &Service{
		userRepo:         userRepo,
		roleRepo:         roleRepo,
		permRepo:         permRepo,
		tokenRepo:        tokenRepo,
		authStateRepo:    authStateRepo,
		authStateCache:   authStateCache,
		merchantUserRepo: merchantUserRepo,
		txManager:        txManager,
		jwtService:       jwtService,
		config:           config,
	}
}

func (s *Service) getTxManager(ctx context.Context) (tx.Manager, error) {
	if s.txManager != nil {
		return s.txManager, nil
	}
	return tenant.GetTxManager(ctx)
}

func (s *Service) requireTenantID(ctx context.Context) (string, error) {
	tenantID := tenant.GetTenantID(ctx)
	if tenantID == "" {
		// Should be prevented by TenantDB middleware; treat as bad request if it happens.
		return "", apperror.NewValidation("tenant is required").
			WithDetail("header", "X-Tenant-ID")
	}
	return tenantID, nil
}

func normalizeAuthVersion(version int64) int64 {
	if version <= 0 {
		return 1
	}
	return version
}

// InvalidateUserAccess bumps a user's auth epoch and clears the in-memory
// validation cache. Existing access tokens become TOKEN_STALE immediately.
func (s *Service) InvalidateUserAccess(ctx context.Context, userID id.ID, reason string) error {
	if err := s.bumpUserAuthVersion(ctx, userID, reason); err != nil {
		return err
	}
	s.invalidateUserAuthCache(ctx, userID)
	return nil
}

// BumpUserAuthVersion increments the user's server-side auth epoch.
// Call InvalidateUserAuthCache after the surrounding transaction commits.
func (s *Service) BumpUserAuthVersion(ctx context.Context, userID id.ID, reason string) error {
	return s.bumpUserAuthVersion(ctx, userID, reason)
}

// InvalidateUserAuthCache clears cached auth state for the user.
func (s *Service) InvalidateUserAuthCache(ctx context.Context, userID id.ID) {
	s.invalidateUserAuthCache(ctx, userID)
}

func (s *Service) bumpUserAuthVersion(ctx context.Context, userID id.ID, reason string) error {
	if s.authStateRepo == nil {
		return nil
	}
	version, err := s.authStateRepo.BumpUserAuthVersion(ctx, userID)
	if err != nil {
		return err
	}
	logger.Info(ctx, "user auth version bumped", "user_id", userID, "version", version, "reason", reason)
	return nil
}

func (s *Service) bumpPolicyVersion(ctx context.Context, reason string) error {
	if err := s.bumpPolicyEpoch(ctx, reason); err != nil {
		return err
	}
	s.invalidatePolicyCache(ctx)
	return nil
}

func (s *Service) bumpPolicyEpoch(ctx context.Context, reason string) error {
	if s.authStateRepo == nil {
		return nil
	}
	version, err := s.authStateRepo.BumpPolicyVersion(ctx)
	if err != nil {
		return err
	}
	logger.Info(ctx, "auth policy version bumped", "version", version, "reason", reason)
	return nil
}

func (s *Service) invalidateUserAuthCache(ctx context.Context, userID id.ID) {
	if tenantID := tenant.GetTenantID(ctx); tenantID != "" && s.authStateCache != nil {
		s.authStateCache.InvalidateUser(tenantID, userID)
	}
}

func (s *Service) invalidateSessionAuthCache(ctx context.Context, sessionID id.ID) {
	if tenantID := tenant.GetTenantID(ctx); tenantID != "" && s.authStateCache != nil {
		s.authStateCache.InvalidateSession(tenantID, sessionID)
	}
}

func (s *Service) invalidatePolicyCache(ctx context.Context) {
	if tenantID := tenant.GetTenantID(ctx); tenantID != "" && s.authStateCache != nil {
		s.authStateCache.InvalidatePolicy(tenantID)
	}
}

// Register registers a new user.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*User, error) {
	if _, err := s.requireTenantID(ctx); err != nil {
		return nil, err
	}

	// Validate email
	if req.Email == "" {
		return nil, apperror.NewValidation("email is required").WithDetail("field", "email")
	}

	// Validate password
	if len(req.Password) < s.config.PasswordMinLength {
		return nil, apperror.NewValidation(
			fmt.Sprintf("password must be at least %d characters", s.config.PasswordMinLength),
		).WithDetail("field", "password")
	}

	// Check if email already exists
	exists, err := s.userRepo.Exists(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("check email exists: %w", err)
	}
	if exists {
		return nil, apperror.NewConflict("email already registered").WithDetail("email", req.Email)
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// Create user
	user := NewUser(req.Email, string(passwordHash))
	user.FirstName = req.FirstName
	user.LastName = req.LastName

	// Save in transaction
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.userRepo.Create(ctx, user); err != nil {
			return fmt.Errorf("create user: %w", err)
		}

		// Assign default role (if exists)
		defaultRole, err := s.roleRepo.GetByCode(ctx, "user")
		if err == nil && defaultRole != nil {
			if err := s.userRepo.AssignRole(ctx, user.ID, defaultRole.ID, id.ID{}); err != nil {
				logger.Warn(ctx, "failed to assign default role", "error", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	logger.Info(ctx, "user registered",
		"user_id", user.ID,
		"email", user.Email)

	return user, nil
}

// Login authenticates user and returns tokens.
func (s *Service) Login(ctx context.Context, creds Credentials, info SessionInfo) (*TokenPair, *User, error) {
	if _, err := s.requireTenantID(ctx); err != nil {
		return nil, nil, err
	}

	// Find user
	user, err := s.userRepo.GetByEmail(ctx, creds.Email)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get user by email", "email", creds.Email, "error", err)
		}
		return nil, nil, apperror.NewUnauthorized("invalid credentials").WithCause(err)
	}
	// Check if can login
	if err := user.CanLogin(); err != nil {
		return nil, nil, err
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(creds.Password)); err != nil {
		// Record failed attempt
		user.RecordFailedLogin(s.config.MaxLoginAttempts, s.config.LockDuration)
		_ = s.userRepo.Update(ctx, user)
		return nil, nil, apperror.NewUnauthorized("invalid credentials")
	}

	// Load roles and permissions
	roles, err := s.userRepo.LoadRoles(ctx, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("load roles: %w", err)
	}
	user.Roles = roles

	permissions, err := s.userRepo.LoadPermissions(ctx, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("load permissions: %w", err)
	}
	user.Permissions = permissions

	var tokens *TokenPair
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		// Record successful login and create session/refresh token atomically.
		user.RecordSuccessfulLogin()
		if err := s.userRepo.Update(ctx, user); err != nil {
			return fmt.Errorf("record successful login: %w", err)
		}

		var genErr error
		tokens, genErr = s.generateTokenPair(ctx, user, info, id.Nil())
		if genErr != nil {
			return fmt.Errorf("generate tokens: %w", genErr)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	logger.Info(ctx, "user logged in",
		"user_id", user.ID,
		"email", user.Email)

	return tokens, user, nil
}

// RefreshToken refreshes access token using refresh token.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string, info SessionInfo) (*TokenPair, error) {
	tokenHash := hashToken(refreshToken)

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}

	var tokens *TokenPair
	var postCommitErr error
	var reusedSessionID id.ID
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		token, err := s.tokenRepo.GetRefreshToken(ctx, tokenHash)
		if err != nil {
			if !apperror.IsNotFound(err) {
				logger.Error(ctx, "failed to get refresh token", "error", err)
			}
			return apperror.NewUnauthorized("invalid refresh token").WithCause(err)
		}

		if token.RevokedAt != nil {
			if s.authStateRepo != nil && !id.IsNil(token.SessionID) {
				if err := s.authStateRepo.RevokeSession(ctx, token.SessionID, "refresh_reuse"); err != nil {
					return err
				}
				reusedSessionID = token.SessionID
			}
			postCommitErr = apperror.NewSessionRevoked()
			return nil
		}
		if !token.IsValid() {
			return apperror.NewUnauthorized("refresh token expired")
		}

		if s.authStateRepo != nil {
			state, err := s.authStateRepo.GetSessionState(ctx, token.UserID, token.SessionID)
			if err != nil {
				return apperror.NewUnauthorized("session not found").WithCause(err)
			}
			if !state.IsValid() {
				return apperror.NewSessionRevoked()
			}
		}

		user, err := s.userRepo.GetByID(ctx, token.UserID)
		if err != nil {
			if !apperror.IsNotFound(err) {
				logger.Error(ctx, "failed to get user by ID for refresh", "user_id", token.UserID, "error", err)
			}
			return apperror.NewUnauthorized("user not found").WithCause(err)
		}

		if err := user.CanLogin(); err != nil {
			return err
		}

		roles, _ := s.userRepo.LoadRoles(ctx, user.ID)
		user.Roles = roles
		permissions, _ := s.userRepo.LoadPermissions(ctx, user.ID)
		user.Permissions = permissions

		if err := s.tokenRepo.RevokeRefreshToken(ctx, token.ID, "refreshed"); err != nil {
			return err
		}

		var genErr error
		tokens, genErr = s.generateTokenPair(ctx, user, info, token.SessionID)
		return genErr
	})
	if err != nil {
		return nil, err
	}
	if !id.IsNil(reusedSessionID) {
		s.invalidateSessionAuthCache(ctx, reusedSessionID)
	}
	if postCommitErr != nil {
		return nil, postCommitErr
	}

	return tokens, nil
}

// Logout revokes all user's refresh tokens.
func (s *Service) Logout(ctx context.Context, userID id.ID) error {
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.tokenRepo.RevokeAllUserTokens(ctx, userID, "logout"); err != nil {
			return err
		}
		if s.authStateRepo != nil {
			if err := s.authStateRepo.RevokeAllUserSessions(ctx, userID, "logout"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if tenantID := tenant.GetTenantID(ctx); tenantID != "" && s.authStateCache != nil {
		s.authStateCache.InvalidateUser(tenantID, userID)
	}
	return nil
}

// AssignRole assigns a role to a user.
func (s *Service) AssignRole(ctx context.Context, userID id.ID, roleCode string) error {
	// Get current user for audit
	currentUser := appctx.GetUser(ctx)
	var grantedBy id.ID
	if currentUser != nil {
		grantedBy, _ = id.Parse(currentUser.UserID)
	}

	// Ensure user exists
	_, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get user for role assignment", "user_id", userID, "error", err)
		}
		return apperror.NewNotFound("user", userID.String()).WithCause(err)
	}

	// Find role
	role, err := s.roleRepo.GetByCode(ctx, roleCode)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get role by code", "role_code", roleCode, "error", err)
		}
		return apperror.NewNotFound("role", roleCode).WithCause(err)
	}

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.userRepo.AssignRole(ctx, userID, role.ID, grantedBy); err != nil {
			return fmt.Errorf("assign role: %w", err)
		}
		if err := s.bumpUserAuthVersion(ctx, userID, "role_assigned"); err != nil {
			return fmt.Errorf("invalidate user access: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	s.invalidateUserAuthCache(ctx, userID)

	logger.Info(ctx, "role assigned",
		"user_id", userID,
		"role", roleCode,
		"granted_by", grantedBy)

	return nil
}

// RevokeRole revokes a role from a user.
func (s *Service) RevokeRole(ctx context.Context, userID id.ID, roleCode string) error {
	// Ensure user exists
	_, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get user for role revocation", "user_id", userID, "error", err)
		}
		return apperror.NewNotFound("user", userID.String()).WithCause(err)
	}

	role, err := s.roleRepo.GetByCode(ctx, roleCode)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get role by code", "role_code", roleCode, "error", err)
		}
		return apperror.NewNotFound("role", roleCode).WithCause(err)
	}

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.userRepo.RevokeRole(ctx, userID, role.ID); err != nil {
			return err
		}
		if err := s.bumpUserAuthVersion(ctx, userID, "role_revoked"); err != nil {
			return fmt.Errorf("invalidate user access: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	s.invalidateUserAuthCache(ctx, userID)
	return nil
}

// GetUserByID retrieves user with roles and permissions.
func (s *Service) GetUserByID(ctx context.Context, userID id.ID) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get user by ID", "user_id", userID, "error", err)
		}
		return nil, apperror.NewNotFound("user", userID.String()).WithCause(err)
	}

	// Load relations
	roles, _ := s.userRepo.LoadRoles(ctx, user.ID)
	user.Roles = roles
	permissions, _ := s.userRepo.LoadPermissions(ctx, user.ID)
	user.Permissions = permissions

	return user, nil
}

// UpdateUser updates user profile fields (admin operation).
func (s *Service) UpdateUser(ctx context.Context, userID id.ID, firstName, lastName *string, isActive, isAdmin *bool) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get user for update", "user_id", userID, "error", err)
		}
		return nil, apperror.NewNotFound("user", userID.String()).WithCause(err)
	}

	if firstName != nil {
		user.FirstName = *firstName
	}
	if lastName != nil {
		user.LastName = *lastName
	}
	authSensitiveChange := (isActive != nil && user.IsActive != *isActive) ||
		(isAdmin != nil && user.IsAdmin != *isAdmin)
	if isActive != nil {
		user.IsActive = *isActive
	}
	if isAdmin != nil {
		user.IsAdmin = *isAdmin
	}

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.userRepo.Update(ctx, user); err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		if authSensitiveChange {
			if err := s.bumpUserAuthVersion(ctx, userID, "user_auth_fields_changed"); err != nil {
				return fmt.Errorf("invalidate user access: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if authSensitiveChange {
		s.invalidateUserAuthCache(ctx, userID)
	}

	// Load relations
	roles, _ := s.userRepo.LoadRoles(ctx, user.ID)
	user.Roles = roles

	logger.Info(ctx, "user updated", "user_id", userID)
	return user, nil
}

// CreateUserByAdmin creates a user by an admin (with optional roles).
func (s *Service) CreateUserByAdmin(ctx context.Context, req RegisterRequest, roleCodes []string) (*User, error) {
	if _, err := s.requireTenantID(ctx); err != nil {
		return nil, err
	}

	if req.Email == "" {
		return nil, apperror.NewValidation("email is required").WithDetail("field", "email")
	}
	if len(req.Password) < s.config.PasswordMinLength {
		return nil, apperror.NewValidation(
			fmt.Sprintf("password must be at least %d characters", s.config.PasswordMinLength),
		).WithDetail("field", "password")
	}

	exists, err := s.userRepo.Exists(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("check email exists: %w", err)
	}
	if exists {
		return nil, apperror.NewConflict("email already registered").WithDetail("email", req.Email)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := NewUser(req.Email, string(passwordHash))
	user.FirstName = req.FirstName
	user.LastName = req.LastName
	user.EmailVerified = true // Admin-created users are implicitly verified

	currentUser := appctx.GetUser(ctx)
	var grantedBy id.ID
	if currentUser != nil {
		grantedBy, _ = id.Parse(currentUser.UserID)
	}

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.userRepo.Create(ctx, user); err != nil {
			return fmt.Errorf("create user: %w", err)
		}

		for _, roleCode := range roleCodes {
			role, err := s.roleRepo.GetByCode(ctx, roleCode)
			if err != nil {
				logger.Warn(ctx, "role not found for admin create", "role_code", roleCode, "error", err)
				continue
			}
			if err := s.userRepo.AssignRole(ctx, user.ID, role.ID, grantedBy); err != nil {
				logger.Warn(ctx, "failed to assign role", "role_code", roleCode, "error", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Load relations
	roles, _ := s.userRepo.LoadRoles(ctx, user.ID)
	user.Roles = roles

	logger.Info(ctx, "user created by admin", "user_id", user.ID, "email", user.Email)
	return user, nil
}

// ListUsers lists users with filtering, including their roles.
func (s *Service) ListUsers(ctx context.Context, filter UserFilter) ([]User, int, error) {
	users, total, err := s.userRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Enrich each user with roles
	for i := range users {
		roles, err := s.userRepo.LoadRoles(ctx, users[i].ID)
		if err != nil {
			return nil, 0, err
		}
		users[i].Roles = roles
	}

	return users, total, nil
}

// ListRoles lists all roles (within tenant database).
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	return s.roleRepo.List(ctx)
}

// ListRolePermissions returns permissions for a specific role.
func (s *Service) ListRolePermissions(ctx context.Context, roleID id.ID) ([]Permission, error) {
	// Ensure role exists
	_, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get role for permissions", "role_id", roleID, "error", err)
		}
		return nil, apperror.NewNotFound("role", roleID.String()).WithCause(err)
	}

	permissions, err := s.roleRepo.LoadPermissions(ctx, roleID)
	if err != nil {
		return nil, fmt.Errorf("load role permissions: %w", err)
	}

	return permissions, nil
}

// ListPermissions lists all permissions.
func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	return s.permRepo.List(ctx)
}

// CreateRole creates a new role.
func (s *Service) CreateRole(ctx context.Context, code, name, description string) (*Role, error) {
	if code == "" {
		return nil, apperror.NewValidation("code is required").WithDetail("field", "code")
	}
	if name == "" {
		return nil, apperror.NewValidation("name is required").WithDetail("field", "name")
	}

	// Check uniqueness
	existing, err := s.roleRepo.GetByCode(ctx, code)
	if err == nil && existing != nil {
		return nil, apperror.NewConflict("role with this code already exists").WithDetail("code", code)
	}

	role := NewRole(code, name)
	role.Description = description

	if err := s.roleRepo.Create(ctx, role); err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}

	logger.Info(ctx, "role created", "role_id", role.ID, "code", code)
	return role, nil
}

// UpdateRole updates a role's name and description.
func (s *Service) UpdateRole(ctx context.Context, roleID id.ID, name, description string) (*Role, error) {
	role, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		return nil, apperror.NewNotFound("role", roleID.String()).WithCause(err)
	}

	if name == "" {
		return nil, apperror.NewValidation("name is required").WithDetail("field", "name")
	}

	role.Name = name
	role.Description = description

	if err := s.roleRepo.Update(ctx, role); err != nil {
		return nil, fmt.Errorf("update role: %w", err)
	}

	logger.Info(ctx, "role updated", "role_id", roleID, "name", name)
	return role, nil
}

// DeleteRole deletes a non-system role, checking for user dependencies.
func (s *Service) DeleteRole(ctx context.Context, roleID id.ID) (int, error) {
	role, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		return 0, apperror.NewNotFound("role", roleID.String()).WithCause(err)
	}

	if role.IsSystem {
		return 0, apperror.NewBusinessRule("CANNOT_DELETE_SYSTEM_ROLE", "Cannot delete system role")
	}

	// Count affected users for the response
	userCount, err := s.roleRepo.CountUsersByRoleID(ctx, roleID)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return 0, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.roleRepo.Delete(ctx, roleID); err != nil {
			return err
		}
		if userCount > 0 {
			if err := s.bumpPolicyEpoch(ctx, "role_deleted"); err != nil {
				return fmt.Errorf("bump policy version: %w", err)
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if userCount > 0 {
		s.invalidatePolicyCache(ctx)
	}

	logger.Info(ctx, "role deleted", "role_id", roleID, "code", role.Code, "affected_users", userCount)
	return userCount, nil
}

// GetRole retrieves a role by ID with permissions and user count.
func (s *Service) GetRole(ctx context.Context, roleID id.ID) (*Role, []Permission, int, error) {
	role, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		return nil, nil, 0, apperror.NewNotFound("role", roleID.String()).WithCause(err)
	}

	permissions, err := s.roleRepo.LoadPermissions(ctx, roleID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("load permissions: %w", err)
	}

	userCount, err := s.roleRepo.CountUsersByRoleID(ctx, roleID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("count users: %w", err)
	}

	return role, permissions, userCount, nil
}

// SetRolePermissions replaces all permissions for a role and bumps the RBAC policy epoch.
func (s *Service) SetRolePermissions(ctx context.Context, roleID id.ID, permissionIDs []id.ID) error {
	// Verify role exists
	_, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		return apperror.NewNotFound("role", roleID.String()).WithCause(err)
	}

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}

	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.roleRepo.SetPermissions(ctx, roleID, permissionIDs); err != nil {
			return err
		}
		if err := s.bumpPolicyEpoch(ctx, "role_permissions_changed"); err != nil {
			return fmt.Errorf("bump policy version: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set role permissions: %w", err)
	}
	s.invalidatePolicyCache(ctx)

	logger.Info(ctx, "role permissions updated", "role_id", roleID, "permission_count", len(permissionIDs))
	return nil
}

// Impersonate generates tokens for a target user (admin-only impersonation).
// The caller must be an admin. Returns tokens that allow acting as the target user.
func (s *Service) Impersonate(ctx context.Context, targetUserID id.ID, info SessionInfo) (*TokenPair, *User, error) {
	if _, err := s.requireTenantID(ctx); err != nil {
		return nil, nil, err
	}

	// Verify caller is admin
	caller := appctx.GetUser(ctx)
	if caller == nil || !caller.IsAdmin {
		return nil, nil, apperror.NewForbidden("only admins can impersonate users")
	}

	// Prevent self-impersonation
	if caller.UserID == targetUserID.String() {
		return nil, nil, apperror.NewValidation("cannot impersonate yourself")
	}

	// Load target user with all relations
	user, err := s.userRepo.GetByID(ctx, targetUserID)
	if err != nil {
		if !apperror.IsNotFound(err) {
			logger.Error(ctx, "failed to get user for impersonation", "target_user_id", targetUserID, "error", err)
		}
		return nil, nil, apperror.NewNotFound("user", targetUserID.String()).WithCause(err)
	}

	if !user.IsActive {
		return nil, nil, apperror.NewValidation("cannot impersonate inactive user")
	}

	// Load roles, permissions, orgs
	roles, _ := s.userRepo.LoadRoles(ctx, user.ID)
	user.Roles = roles
	permissions, _ := s.userRepo.LoadPermissions(ctx, user.ID)
	user.Permissions = permissions

	// Generate tokens for target user
	tokens, err := s.generateTokenPair(ctx, user, info, id.Nil())
	if err != nil {
		return nil, nil, fmt.Errorf("generate impersonation tokens: %w", err)
	}

	logger.Info(ctx, "user impersonated",
		"admin_id", caller.UserID,
		"target_user_id", targetUserID,
		"target_email", user.Email)

	return tokens, user, nil
}

// generateTokenPair creates access and refresh tokens. When sessionID is zero,
// it creates a new server-side auth session; otherwise it rotates the refresh
// token within the existing session.
func (s *Service) generateTokenPair(ctx context.Context, user *User, info SessionInfo, sessionID id.ID) (*TokenPair, error) {
	tenantID, err := s.requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if s.authStateRepo == nil {
		return nil, apperror.NewInternal(fmt.Errorf("auth state repository is not configured"))
	}

	// Extract role codes
	roleCodes := make([]string, len(user.Roles))
	for i, r := range user.Roles {
		roleCodes[i] = r.Code
	}

	// Load merchant associations for portal JWT claims.
	// Best-effort: if merchantUserRepo is nil or query fails, skip portal claims.
	var merchantIDs []string
	var merchantRoles map[string]int
	if s.merchantUserRepo != nil {
		assocs, err := s.merchantUserRepo.ListByUser(ctx, user.ID)
		if err != nil {
			logger.Warn(ctx, "failed to load merchant associations for JWT",
				"user_id", user.ID, "error", err)
		} else if len(assocs) > 0 {
			merchantIDs = make([]string, 0, len(assocs))
			merchantRoles = make(map[string]int, len(assocs))
			for _, a := range assocs {
				merchantID := a.MerchantID.String()
				merchantIDs = append(merchantIDs, merchantID)
				merchantRoles[merchantID] = int(a.Role)
			}
		}
	}

	// Set portal claims on user for DTO serialization.
	user.MerchantIDs = merchantIDs

	userAuthVersion := normalizeAuthVersion(user.AuthVersion)
	policyVersion, err := s.authStateRepo.GetCurrentPolicyVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("get auth policy version: %w", err)
	}

	refreshExpiresAt := time.Now().Add(s.config.RefreshTokenExpiry)
	if id.IsNil(sessionID) {
		sessionID = id.New()
		session := &AuthSession{
			ID:              sessionID,
			UserID:          user.ID,
			UserAuthVersion: userAuthVersion,
			PolicyVersion:   policyVersion,
			CreatedAt:       time.Now(),
			ExpiresAt:       refreshExpiresAt,
			UserAgent:       info.UserAgent,
			IPAddress:       info.IPAddress,
		}
		if err := s.authStateRepo.CreateSession(ctx, session); err != nil {
			return nil, fmt.Errorf("create auth session: %w", err)
		}
	} else if err := s.authStateRepo.ExtendSession(ctx, sessionID, refreshExpiresAt, info); err != nil {
		return nil, fmt.Errorf("extend auth session: %w", err)
	}

	accessToken, expiresAt, err := s.jwtService.GenerateAccessToken(
		user.ID.String(), tenantID, sessionID.String(), user.Email,
		userAuthVersion, policyVersion,
		roleCodes, user.Permissions, user.IsAdmin,
		merchantIDs, merchantRoles,
	)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	// Generate refresh token
	refreshTokenRaw, err := generateRandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshTokenHash := hashToken(refreshTokenRaw)

	// Save refresh token
	refreshToken := &RefreshToken{
		ID:        id.New(),
		UserID:    user.ID,
		SessionID: sessionID,
		TokenHash: refreshTokenHash,
		ExpiresAt: refreshExpiresAt,
		CreatedAt: time.Now(),
		UserAgent: info.UserAgent,
		IPAddress: info.IPAddress,
	}

	if err := s.tokenRepo.SaveRefreshToken(ctx, refreshToken); err != nil {
		return nil, fmt.Errorf("save refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenRaw,
		ExpiresAt:    expiresAt,
		TokenType:    "Bearer",
	}, nil
}

// hashToken creates SHA256 hash of token.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// generateRandomToken generates a random token string.
func generateRandomToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
