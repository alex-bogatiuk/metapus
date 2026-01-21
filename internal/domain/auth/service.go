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
	"metapus/pkg/logger"
)

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
	userRepo   UserRepository
	roleRepo   RoleRepository
	permRepo   PermissionRepository
	tokenRepo  TokenRepository
	txManager  tx.Manager
	jwtService *JWTService
	config     ServiceConfig
}

// NewService creates a new auth service.
func NewService(
	userRepo UserRepository,
	roleRepo RoleRepository,
	permRepo PermissionRepository,
	tokenRepo TokenRepository,
	txManager tx.Manager,
	jwtService *JWTService,
	config ServiceConfig,
) *Service {
	return &Service{
		userRepo:   userRepo,
		roleRepo:   roleRepo,
		permRepo:   permRepo,
		tokenRepo:  tokenRepo,
		txManager:  txManager,
		jwtService: jwtService,
		config:     config,
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
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
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
func (s *Service) Login(ctx context.Context, creds Credentials) (*TokenPair, *User, error) {
	if _, err := s.requireTenantID(ctx); err != nil {
		return nil, nil, err
	}

	// Find user
	user, err := s.userRepo.GetByEmail(ctx, creds.Email)
	if err != nil {
		return nil, nil, apperror.NewUnauthorized("invalid credentials")
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

	orgIDs, err := s.userRepo.LoadOrganizations(ctx, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("load organizations: %w", err)
	}
	user.OrgIDs = orgIDs

	// Generate tokens
	tokens, err := s.generateTokenPair(ctx, user)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	// Record successful login
	user.RecordSuccessfulLogin()
	_ = s.userRepo.Update(ctx, user)

	logger.Info(ctx, "user logged in",
		"user_id", user.ID,
		"email", user.Email)

	return tokens, user, nil
}

// RefreshToken refreshes access token using refresh token.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	// Hash token to lookup
	tokenHash := hashToken(refreshToken)

	// Find token
	token, err := s.tokenRepo.GetRefreshToken(ctx, tokenHash)
	if err != nil {
		return nil, apperror.NewUnauthorized("invalid refresh token")
	}

	// Validate token
	if !token.IsValid() {
		return nil, apperror.NewUnauthorized("refresh token expired or revoked")
	}

	// Get user
	user, err := s.userRepo.GetByID(ctx, token.UserID)
	if err != nil {
		return nil, apperror.NewUnauthorized("user not found")
	}

	// Check if user can login
	if err := user.CanLogin(); err != nil {
		return nil, err
	}

	// Load roles and permissions
	roles, _ := s.userRepo.LoadRoles(ctx, user.ID)
	user.Roles = roles
	permissions, _ := s.userRepo.LoadPermissions(ctx, user.ID)
	user.Permissions = permissions
	orgIDs, _ := s.userRepo.LoadOrganizations(ctx, user.ID)
	user.OrgIDs = orgIDs

	// Revoke old refresh token
	_ = s.tokenRepo.RevokeRefreshToken(ctx, token.ID, "refreshed")

	// Generate new token pair
	return s.generateTokenPair(ctx, user)
}

// Logout revokes all user's refresh tokens.
func (s *Service) Logout(ctx context.Context, userID id.ID) error {
	return s.tokenRepo.RevokeAllUserTokens(ctx, userID, "logout")
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
		return apperror.NewNotFound("user", userID.String())
	}

	// Find role
	role, err := s.roleRepo.GetByCode(ctx, roleCode)
	if err != nil {
		return apperror.NewNotFound("role", roleCode)
	}

	// Assign role
	if err := s.userRepo.AssignRole(ctx, userID, role.ID, grantedBy); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}

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
		return apperror.NewNotFound("user", userID.String())
	}

	role, err := s.roleRepo.GetByCode(ctx, roleCode)
	if err != nil {
		return apperror.NewNotFound("role", roleCode)
	}

	return s.userRepo.RevokeRole(ctx, userID, role.ID)
}

// GetUserByID retrieves user with roles and permissions.
func (s *Service) GetUserByID(ctx context.Context, userID id.ID) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, apperror.NewNotFound("user", userID.String())
	}

	// Load relations
	roles, _ := s.userRepo.LoadRoles(ctx, user.ID)
	user.Roles = roles
	permissions, _ := s.userRepo.LoadPermissions(ctx, user.ID)
	user.Permissions = permissions
	orgIDs, _ := s.userRepo.LoadOrganizations(ctx, user.ID)
	user.OrgIDs = orgIDs

	return user, nil
}

// ListUsers lists users with filtering.
func (s *Service) ListUsers(ctx context.Context, filter UserFilter) ([]User, int, error) {
	return s.userRepo.List(ctx, filter)
}

// ListRoles lists all roles (within tenant database).
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	return s.roleRepo.List(ctx)
}

// ListPermissions lists all permissions.
func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	return s.permRepo.List(ctx)
}

// CreateRole creates a new role.
func (s *Service) CreateRole(ctx context.Context, code, name, description string) (*Role, error) {
	role := NewRole(code, name)
	role.Description = description

	if err := s.roleRepo.Create(ctx, role); err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}

	return role, nil
}

// generateTokenPair creates access and refresh tokens.
func (s *Service) generateTokenPair(ctx context.Context, user *User) (*TokenPair, error) {
	tenantID, err := s.requireTenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Extract role codes
	roleCodes := make([]string, len(user.Roles))
	for i, r := range user.Roles {
		roleCodes[i] = r.Code
	}

	// Generate access token
	accessToken, expiresAt, err := s.jwtService.GenerateAccessToken(user.ID.String(), tenantID, user.Email, roleCodes, user.Permissions, user.OrgIDs, user.IsAdmin)
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
		TokenHash: refreshTokenHash,
		ExpiresAt: time.Now().Add(s.config.RefreshTokenExpiry),
		CreatedAt: time.Now(),
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
