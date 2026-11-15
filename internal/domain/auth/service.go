// Package auth provides authentication and authorization domain logic.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"time"

	"golang.org/x/crypto/bcrypt"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/notifications"
	"metapus/pkg/logger"
)

// BcryptCost is the bcrypt work factor for password hashing.
// OWASP recommends >= 12. Higher values increase brute-force resistance
// at the cost of ~150ms additional latency per login.
const BcryptCost = 12

// ServiceConfig holds auth service configuration.
type ServiceConfig struct {
	MaxLoginAttempts    int
	LockDuration        time.Duration
	PasswordMinLength   int
	RefreshTokenExpiry  time.Duration
	AdminMFAMode        MFAMode
	AdminMFAGracePeriod time.Duration
	AdminStepUpTTL      time.Duration
	AdminSecurityMode   AdminSecurityMode
	AdminAllowedCIDRs   []netip.Prefix
	AdminIdleTimeout    time.Duration
	AdminAbsoluteMaxAge time.Duration
	MFAEncryptionKey    []byte
	MFAIssuer           string
}

// DefaultServiceConfig returns default configuration.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		MaxLoginAttempts:    5,
		LockDuration:        15 * time.Minute,
		PasswordMinLength:   8,
		RefreshTokenExpiry:  7 * 24 * time.Hour, // 7 days
		AdminMFAMode:        MFAModeOff,
		AdminMFAGracePeriod: 168 * time.Hour,
		AdminStepUpTTL:      5 * time.Minute,
		AdminSecurityMode:   AdminSecurityOff,
		AdminIdleTimeout:    30 * time.Minute,
		AdminAbsoluteMaxAge: 12 * time.Hour,
		MFAIssuer:           "Metapus",
	}
}

// Service provides authentication and authorization logic.
type Service struct {
	userRepo         UserRepository
	roleRepo         RoleRepository
	permRepo         PermissionRepository
	tokenRepo        TokenRepository
	mfaRepo          MFARepository
	authStateRepo    AuthStateRepository
	authStateCache   *AuthStateCache
	txManager        tx.Manager
	jwtService       AccessTokenIssuer
	eventWriter      eventlog.Writer
	notificationRepo notifications.Repository
	config           ServiceConfig
	qrCodeEncoder    QRCodeEncoder

	accessScopeProviders []AccessScopeProvider
}

// NewService creates a new auth service.
func NewService(
	userRepo UserRepository,
	roleRepo RoleRepository,
	permRepo PermissionRepository,
	tokenRepo TokenRepository,
	mfaRepo MFARepository,
	authStateRepo AuthStateRepository,
	authStateCache *AuthStateCache,
	txManager tx.Manager,
	jwtService AccessTokenIssuer,
	eventWriter eventlog.Writer,
	notificationRepo notifications.Repository,
	config ServiceConfig,
	options ...ServiceOption,
) *Service {
	s := &Service{
		userRepo:         userRepo,
		roleRepo:         roleRepo,
		permRepo:         permRepo,
		tokenRepo:        tokenRepo,
		mfaRepo:          mfaRepo,
		authStateRepo:    authStateRepo,
		authStateCache:   authStateCache,
		txManager:        txManager,
		jwtService:       jwtService,
		eventWriter:      eventWriter,
		notificationRepo: notificationRepo,
		config:           config,
	}
	for _, option := range options {
		option(s)
	}
	return s
}

type ServiceOption func(*Service)

func WithQRCodeEncoder(encoder QRCodeEncoder) ServiceOption {
	return func(s *Service) { s.qrCodeEncoder = encoder }
}

// AccessScopeProvider supplies one kind of object-level grants that a
// configuration embeds in access tokens (for example, merchant portal access).
type AccessScopeProvider interface {
	AccessScopeKind() string
	AccessGrants(ctx context.Context, userID id.ID) (map[string]int, error)
}

// WithAccessScopeProvider registers a configuration's access scope provider.
func WithAccessScopeProvider(provider AccessScopeProvider) ServiceOption {
	return func(s *Service) {
		if provider != nil {
			s.accessScopeProviders = append(s.accessScopeProviders, provider)
		}
	}
}

// loadAccessScopes collects configuration grants for access token claims.
// Best-effort: a failing provider is logged and its kind is left out.
func (s *Service) loadAccessScopes(ctx context.Context, userID id.ID) appctx.AccessScopes {
	var scopes appctx.AccessScopes
	for _, provider := range s.accessScopeProviders {
		kind := provider.AccessScopeKind()
		grants, err := provider.AccessGrants(ctx, userID)
		if err != nil {
			logger.Warn(ctx, "failed to load access scope for JWT",
				"user_id", userID, "scope_kind", kind, "error", err)
			continue
		}
		if len(grants) == 0 {
			continue
		}
		if scopes == nil {
			scopes = make(appctx.AccessScopes, len(s.accessScopeProviders))
		}
		scopes[kind] = grants
	}
	return scopes
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

// Login verifies password and either returns final tokens or an MFA continuation.
func (s *Service) Login(ctx context.Context, creds Credentials, info SessionInfo) (*LoginResult, error) {
	if _, err := s.requireTenantID(ctx); err != nil {
		return nil, err
	}

	user, err := s.userRepo.GetByEmail(ctx, creds.Email)
	if err != nil {
		return nil, lookupError("get user by email", err, apperror.NewUnauthorized("invalid credentials"))
	}

	// Load roles before bcrypt so admin CIDR enforcement can run first.
	roles, err := s.userRepo.LoadRoles(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}
	user.Roles = roles

	if err := s.enforceAdminCIDR(ctx, user, info); err != nil {
		return nil, err
	}
	if err := user.CanLogin(); err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(creds.Password)); err != nil {
		// Record failed attempt
		user.RecordFailedLogin(s.config.MaxLoginAttempts, s.config.LockDuration)
		_ = s.userRepo.Update(ctx, user)
		s.emitSecurityEvent(ctx, eventlog.EventSecurityPasswordDenied, eventlog.SeverityWarning, user.ID, user.Email, info.IPAddress,
			fmt.Sprintf("Password denied for user %s", user.Email),
			map[string]any{"email": user.Email},
		)
		return nil, apperror.NewUnauthorized("invalid credentials")
	}

	permissions, err := s.userRepo.LoadPermissions(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("load permissions: %w", err)
	}
	user.Permissions = permissions
	if err := s.enrichMFAStatus(ctx, user); err != nil {
		return nil, err
	}

	if s.mfaEnabledFor(user) {
		rec, err := s.mfaRepo.GetTOTP(ctx, user.ID)
		if err != nil && !apperror.IsNotFound(err) {
			return nil, err
		}
		if err == nil && rec.Enabled {
			txm, txErr := s.getTxManager(ctx)
			if txErr != nil {
				return nil, apperror.NewInternal(txErr).WithDetail("missing", "tx_manager")
			}
			var ch *MFAChallenge
			if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
				var createErr error
				ch, createErr = s.createMFAChallenge(ctx, user.ID, "login", info)
				return createErr
			}); err != nil {
				return nil, err
			}
			s.emitSecurityEvent(ctx, eventlog.EventSecurityMFARequired, eventlog.SeverityInfo, user.ID, user.Email, info.IPAddress,
				fmt.Sprintf("MFA required for user %s", user.Email), map[string]any{"email": user.Email})
			return &LoginResult{
				Status:             LoginStatusMFARequired,
				User:               user,
				ChallengeID:        ch.ID,
				ChallengeExpiresAt: ch.ExpiresAt,
			}, nil
		}

		if s.config.AdminMFAMode == MFAModeRequired {
			allowed, rec, err := s.shouldAllowAdminGrace(ctx, user, time.Now().UTC())
			if err != nil {
				return nil, err
			}
			user.MFAGraceEndsAt = rec.RequiredAfter
			user.MFARequired = true
			if !allowed {
				txm, txErr := s.getTxManager(ctx)
				if txErr != nil {
					return nil, apperror.NewInternal(txErr).WithDetail("missing", "tx_manager")
				}
				var ch *MFAChallenge
				if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
					var createErr error
					ch, createErr = s.createMFAChallenge(ctx, user.ID, "enrollment", info)
					return createErr
				}); err != nil {
					return nil, err
				}
				s.emitSecurityEvent(ctx, eventlog.EventSecurityMFARequired, eventlog.SeverityWarning, user.ID, user.Email, info.IPAddress,
					fmt.Sprintf("MFA enrollment required for user %s", user.Email), map[string]any{"email": user.Email})
				return &LoginResult{
					Status:             LoginStatusMFAEnrollmentRequired,
					User:               user,
					ChallengeID:        ch.ID,
					ChallengeExpiresAt: ch.ExpiresAt,
				}, nil
			}
			user.MFAPending = true
			return s.finalizeLogin(ctx, user, info, TokenSessionOptions{MFAPending: true})
		}

		// Optional mode: unenrolled admins can sign in, but session carries MFA pending.
		user.MFAPending = true
		return s.finalizeLogin(ctx, user, info, TokenSessionOptions{MFAPending: true})
	}

	logger.Info(ctx, "user logged in",
		"user_id", user.ID,
		"email", user.Email)

	return s.finalizeLogin(ctx, user, info, TokenSessionOptions{})
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
	var refreshedSessionID id.ID
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		token, err := s.tokenRepo.GetRefreshToken(ctx, tokenHash)
		if err != nil {
			return lookupError("get refresh token", err, apperror.NewUnauthorized("invalid refresh token"))
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

		var state *AuthSessionState
		if s.authStateRepo != nil {
			var stateErr error
			state, stateErr = s.authStateRepo.GetSessionState(ctx, token.UserID, token.SessionID)
			if stateErr != nil {
				return lookupError("get refresh session state", stateErr, apperror.NewUnauthorized("session not found"))
			}
			if !state.IsValid() {
				return apperror.NewSessionRevoked()
			}
		}

		user, err := s.userRepo.GetByID(ctx, token.UserID)
		if err != nil {
			return lookupError("get user by ID for refresh", err, apperror.NewUnauthorized("user not found"))
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
		opts := TokenSessionOptions{}
		if state != nil {
			opts = TokenSessionOptions{
				MFAVerifiedAt: state.MFAVerifiedAt,
				MFAMethod:     state.MFAMethod,
				MFAPending:    state.MFAPending,
			}
		}
		tokens, genErr = s.generateTokenPair(ctx, user, info, token.SessionID, opts)
		if genErr == nil {
			refreshedSessionID = token.SessionID
		}
		return genErr
	})
	if err != nil {
		return nil, err
	}
	if !id.IsNil(reusedSessionID) {
		s.invalidateSessionAuthCache(ctx, reusedSessionID)
	}
	if !id.IsNil(refreshedSessionID) {
		s.invalidateSessionAuthCache(ctx, refreshedSessionID)
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
	currentUser := appctx.GetUser(ctx)
	var grantedBy id.ID
	if currentUser != nil {
		grantedBy, _ = id.Parse(currentUser.UserID)
	}

	_, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return lookupError("get user for role assignment", err, apperror.NewNotFound("user", userID.String()))
	}

	role, err := s.roleRepo.GetByCode(ctx, roleCode)
	if err != nil {
		return lookupError("get role by code", err, apperror.NewNotFound("role", roleCode))
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
	_, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return lookupError("get user for role revocation", err, apperror.NewNotFound("user", userID.String()))
	}

	role, err := s.roleRepo.GetByCode(ctx, roleCode)
	if err != nil {
		return lookupError("get role by code", err, apperror.NewNotFound("role", roleCode))
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
		return nil, lookupError("get user by ID", err, apperror.NewNotFound("user", userID.String()))
	}

	roles, _ := s.userRepo.LoadRoles(ctx, user.ID)
	user.Roles = roles
	permissions, _ := s.userRepo.LoadPermissions(ctx, user.ID)
	user.Permissions = permissions
	if err := s.enrichMFAStatus(ctx, user); err != nil {
		return nil, err
	}
	if userCtx := appctx.GetUser(ctx); userCtx != nil && userCtx.UserID == user.ID.String() {
		user.MFAPending = userCtx.MFAPending
		user.MFAVerifiedAt = userCtx.MFAVerifiedAt
	}

	return user, nil
}

// UpdateUser updates user profile fields (admin operation).
func (s *Service) UpdateUser(ctx context.Context, userID id.ID, firstName, lastName *string, isActive, isAdmin *bool) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, lookupError("get user for update", err, apperror.NewNotFound("user", userID.String()))
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

	if len(users) == 0 {
		return users, total, nil
	}
	userIDs := make([]id.ID, len(users))
	for i := range users {
		userIDs[i] = users[i].ID
	}
	roles, err := s.userRepo.LoadRolesForUsers(ctx, userIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("load user roles: %w", err)
	}
	var statuses map[id.ID]MFAStatus
	if s.mfaRepo != nil {
		statuses, err = s.mfaRepo.LoadMFAStatuses(ctx, userIDs)
		if err != nil {
			return nil, 0, fmt.Errorf("load user MFA statuses: %w", err)
		}
	}
	for i := range users {
		users[i].Roles = roles[users[i].ID]
		s.applyMFAStatus(&users[i], statuses[users[i].ID])
	}

	return users, total, nil
}

// ListRoles lists all roles (within tenant database).
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	return s.roleRepo.List(ctx)
}

// ListRolePermissions returns permissions for a specific role.
func (s *Service) ListRolePermissions(ctx context.Context, roleID id.ID) ([]Permission, error) {
	_, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		return nil, lookupError("get role for permissions", err, apperror.NewNotFound("role", roleID.String()))
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
		return nil, lookupError("get role by ID", err, apperror.NewNotFound("role", roleID.String()))
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
		return 0, lookupError("get role by ID", err, apperror.NewNotFound("role", roleID.String()))
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
		return nil, nil, 0, lookupError("get role by ID", err, apperror.NewNotFound("role", roleID.String()))
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
	_, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		return lookupError("get role by ID", err, apperror.NewNotFound("role", roleID.String()))
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

	if caller.UserID == targetUserID.String() {
		return nil, nil, apperror.NewValidation("cannot impersonate yourself")
	}

	// Load target user with all relations
	user, err := s.userRepo.GetByID(ctx, targetUserID)
	if err != nil {
		return nil, nil, lookupError("get user for impersonation", err, apperror.NewNotFound("user", targetUserID.String()))
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
	tokens, err := s.generateTokenPair(ctx, user, info, id.Nil(), TokenSessionOptions{})
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
func (s *Service) generateTokenPair(ctx context.Context, user *User, info SessionInfo, sessionID id.ID, opts TokenSessionOptions) (*TokenPair, error) {
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

	// Configuration grants travel in the access token and back to the client.
	accessScopes := s.loadAccessScopes(ctx, user.ID)
	user.AccessScopes = accessScopes

	userAuthVersion := normalizeAuthVersion(user.AuthVersion)
	policyVersion, err := s.authStateRepo.GetCurrentPolicyVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("get auth policy version: %w", err)
	}

	now := time.Now().UTC()
	refreshExpiresAt := now.Add(s.config.RefreshTokenExpiry)
	var idleExpiresAt *time.Time
	var absoluteExpiresAt *time.Time
	if user.IsAdminPrincipal() {
		if id.IsNil(sessionID) {
			idle := now.Add(s.config.AdminIdleTimeout)
			absolute := now.Add(s.config.AdminAbsoluteMaxAge)
			if absolute.Before(idle) {
				idle = absolute
			}
			refreshExpiresAt = idle
			idleExpiresAt = &idle
			absoluteExpiresAt = &absolute
		} else {
			state, err := s.authStateRepo.GetSessionState(ctx, user.ID, sessionID)
			if err != nil {
				return nil, lookupError("get session state", err, apperror.NewSessionRevoked())
			}
			absolute := now.Add(s.config.AdminAbsoluteMaxAge)
			if state.AbsoluteExpiresAt != nil {
				absolute = *state.AbsoluteExpiresAt
			}
			if !now.Before(absolute) {
				return nil, apperror.NewSessionRevoked()
			}
			idle := now.Add(s.config.AdminIdleTimeout)
			if absolute.Before(idle) {
				idle = absolute
			}
			refreshExpiresAt = idle
			idleExpiresAt = &idle
			absoluteExpiresAt = &absolute
		}
	}
	if id.IsNil(sessionID) {
		sessionID = id.New()
		session := &AuthSession{
			ID:                sessionID,
			UserID:            user.ID,
			UserAuthVersion:   userAuthVersion,
			PolicyVersion:     policyVersion,
			CreatedAt:         now,
			ExpiresAt:         refreshExpiresAt,
			UserAgent:         info.UserAgent,
			IPAddress:         info.IPAddress,
			MFAVerifiedAt:     opts.MFAVerifiedAt,
			MFAMethod:         opts.MFAMethod,
			MFAPending:        opts.MFAPending,
			IdleExpiresAt:     idleExpiresAt,
			AbsoluteExpiresAt: absoluteExpiresAt,
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
		accessScopes,
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
		CacheScope: CacheScope{
			TenantID: tenantID, PrincipalID: user.ID.String(), SessionID: sessionID.String(),
			UserVersion: fmt.Sprint(userAuthVersion), PolicyVersion: fmt.Sprint(policyVersion),
		},
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
