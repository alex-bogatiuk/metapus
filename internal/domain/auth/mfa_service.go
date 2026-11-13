package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/notifications"
	"metapus/pkg/logger"
)

const (
	_mfaChallengeTTL   = 10 * time.Minute
	_totpPeriodSeconds = 30
	_totpSkewSteps     = 1
	_totpMaxFailures   = 5
	_totpFailureWindow = 15 * time.Minute
	_totpLockDuration  = 15 * time.Minute
)

func (s *Service) emitSecurityEvent(ctx context.Context, eventType eventlog.EventType, severity eventlog.Severity, userID id.ID, email, clientIP, message string, details map[string]any) {
	if s == nil || s.eventWriter == nil {
		return
	}
	if details == nil {
		details = map[string]any{}
	}
	event := eventlog.Event{
		Category:  eventlog.CategorySecurity,
		Severity:  severity,
		EventType: eventType,
		Source:    "api",
		ClientIP:  clientIP,
		Message:   message,
		Details:   details,
	}
	if !id.IsNil(userID) {
		event.UserID = userID.String()
	}
	if err := s.eventWriter.Write(ctx, event); err != nil {
		logger.Warn(ctx, "eventlog: failed to write security event", "eventType", eventType, "error", err)
	}
}

func (s *Service) adminIPAllowed(clientIP string) bool {
	if s.config.AdminSecurityMode == AdminSecurityOff {
		return true
	}
	if len(s.config.AdminAllowedCIDRs) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range s.config.AdminAllowedCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (s *Service) enforceAdminCIDR(ctx context.Context, user *User, info SessionInfo) error {
	if user == nil || !user.IsAdminPrincipal() || s.config.AdminSecurityMode == AdminSecurityOff {
		return nil
	}
	allowed := s.adminIPAllowed(info.IPAddress)
	if allowed {
		return nil
	}

	eventType := eventlog.EventSecurityAdminIPWarn
	severity := eventlog.SeverityWarning
	if s.config.AdminSecurityMode == AdminSecurityEnforce {
		eventType = eventlog.EventSecurityAdminIPDenied
		severity = eventlog.SeverityCritical
	}
	s.emitSecurityEvent(ctx, eventType, severity, user.ID, user.Email, info.IPAddress,
		fmt.Sprintf("Admin login from non-allowlisted IP: %s", user.Email),
		map[string]any{"email": user.Email, "ip": info.IPAddress, "mode": string(s.config.AdminSecurityMode)},
	)
	if s.config.AdminSecurityMode == AdminSecurityEnforce {
		return apperror.NewAdminIPDenied()
	}
	return nil
}

func (s *Service) mfaEnabledFor(user *User) bool {
	return user != nil && user.IsAdminPrincipal() && s.config.AdminMFAMode != MFAModeOff && s.mfaRepo != nil
}

func (s *Service) applyMFAStatus(user *User, status MFAStatus) {
	user.MFARequired = s.mfaRepo != nil && user.IsAdminPrincipal() && s.config.AdminMFAMode == MFAModeRequired
	user.MFAEnabled = status.Enabled
	user.MFAGraceEndsAt = status.RequiredAfter
}

func (s *Service) enrichMFAStatus(ctx context.Context, user *User) error {
	if user == nil || s.mfaRepo == nil {
		return nil
	}
	statuses, err := s.mfaRepo.LoadMFAStatuses(ctx, []id.ID{user.ID})
	if err != nil {
		return fmt.Errorf("load user MFA status: %w", err)
	}
	s.applyMFAStatus(user, statuses[user.ID])
	return nil
}

func (s *Service) shouldAllowAdminGrace(ctx context.Context, user *User, now time.Time) (bool, *TOTPRecord, error) {
	if s.config.AdminMFAMode != MFAModeRequired {
		return true, nil, nil
	}
	requiredAfter := now.Add(s.config.AdminMFAGracePeriod)
	rec, err := s.mfaRepo.EnsureTOTPRequiredAfter(ctx, user.ID, requiredAfter)
	if err != nil {
		return false, nil, err
	}
	if rec.RequiredAfter == nil {
		return false, rec, nil
	}
	return now.Before(*rec.RequiredAfter), rec, nil
}

func (s *Service) createMFAChallenge(ctx context.Context, userID id.ID, challengeType string, info SessionInfo) (*MFAChallenge, error) {
	ch := &MFAChallenge{
		ID:            id.New(),
		UserID:        userID,
		ChallengeType: challengeType,
		ExpiresAt:     time.Now().UTC().Add(_mfaChallengeTTL),
		UserAgent:     info.UserAgent,
		IPAddress:     info.IPAddress,
	}
	if err := s.mfaRepo.CreateChallenge(ctx, ch); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Service) validateChallenge(ch *MFAChallenge, expectedType string) error {
	if ch == nil || ch.ChallengeType != expectedType || ch.ConsumedAt != nil || !time.Now().Before(ch.ExpiresAt) {
		return apperror.NewMFADenied("MFA challenge is invalid or expired")
	}
	return nil
}

func (s *Service) encryptMFASecret(userID id.ID, plaintext string) ([]byte, []byte, error) {
	if len(s.config.MFAEncryptionKey) != 32 {
		return nil, nil, apperror.NewInternal(fmt.Errorf("AUTH_MFA_ENCRYPTION_KEY must decode to 32 bytes"))
	}
	block, err := aes.NewCipher(s.config.MFAEncryptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create mfa cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create mfa gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate mfa nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), []byte(userID.String()))
	return ciphertext, nonce, nil
}

func (s *Service) decryptMFASecret(userID id.ID, ciphertext, nonce []byte) (string, error) {
	if len(s.config.MFAEncryptionKey) != 32 {
		return "", apperror.NewInternal(fmt.Errorf("AUTH_MFA_ENCRYPTION_KEY must decode to 32 bytes"))
	}
	block, err := aes.NewCipher(s.config.MFAEncryptionKey)
	if err != nil {
		return "", fmt.Errorf("create mfa cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create mfa gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(userID.String()))
	if err != nil {
		return "", apperror.NewMFADenied("MFA secret cannot be decrypted")
	}
	return string(plaintext), nil
}

func matchTOTPCode(secret, code string, now time.Time) (int64, bool, error) {
	baseStep := now.Unix() / _totpPeriodSeconds
	for step := baseStep - _totpSkewSteps; step <= baseStep+_totpSkewSteps; step++ {
		ok, err := totp.ValidateCustom(code, secret, time.Unix(step*_totpPeriodSeconds, 0), totp.ValidateOpts{
			Period:    _totpPeriodSeconds,
			Skew:      0,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			return 0, false, err
		}
		if ok {
			return step, true, nil
		}
	}
	return 0, false, nil
}

func (s *Service) verifyTOTPProof(ctx context.Context, user *User, code string, info SessionInfo) error {
	if s.mfaRepo == nil {
		return apperror.NewMFADenied("MFA is not configured")
	}
	rec, err := s.mfaRepo.GetTOTP(ctx, user.ID)
	if err != nil || rec == nil || !rec.Enabled {
		return apperror.NewMFANotEnrolled()
	}
	now := time.Now().UTC()
	if rec.LockedUntil != nil && now.Before(*rec.LockedUntil) {
		return apperror.NewMFALocked("MFA verification is temporarily locked")
	}
	secret, err := s.decryptMFASecret(user.ID, rec.SecretCiphertext, rec.SecretNonce)
	if err != nil {
		return err
	}
	matchedStep, ok, err := matchTOTPCode(secret, strings.TrimSpace(code), now)
	if err != nil {
		return apperror.NewMFADenied("invalid TOTP code").WithCause(err)
	}
	if !ok {
		_, locked, recordErr := s.mfaRepo.RecordTOTPFailure(ctx, user.ID, now, _totpMaxFailures, _totpFailureWindow, _totpLockDuration)
		if recordErr != nil {
			return recordErr
		}
		eventType := eventlog.EventSecurityMFADenied
		severity := eventlog.SeverityWarning
		if locked {
			eventType = eventlog.EventSecurityMFABruteforce
			severity = eventlog.SeverityCritical
		}
		s.emitSecurityEvent(ctx, eventType, severity, user.ID, user.Email, info.IPAddress,
			fmt.Sprintf("MFA denied for user %s", user.Email),
			map[string]any{"email": user.Email, "locked": locked},
		)
		if locked {
			return apperror.NewMFALocked("MFA verification is temporarily locked")
		}
		return apperror.NewMFADenied("invalid TOTP code")
	}
	if err := s.mfaRepo.UpdateTOTPLastUsed(ctx, user.ID, matchedStep); err != nil {
		s.emitSecurityEvent(ctx, eventlog.EventSecurityMFADenied, eventlog.SeverityWarning, user.ID, user.Email, info.IPAddress,
			fmt.Sprintf("MFA replay denied for user %s", user.Email),
			map[string]any{"email": user.Email, "timeStep": matchedStep},
		)
		return err
	}
	if err := s.mfaRepo.ResetTOTPFailures(ctx, user.ID); err != nil {
		return err
	}
	return nil
}

func (s *Service) loadUserWithAuth(ctx context.Context, userID id.ID) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	roles, err := s.userRepo.LoadRoles(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}
	user.Roles = roles
	permissions, err := s.userRepo.LoadPermissions(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("load permissions: %w", err)
	}
	user.Permissions = permissions
	if err := s.enrichMFAStatus(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) finalizeLogin(ctx context.Context, user *User, info SessionInfo, opts TokenSessionOptions) (*LoginResult, error) {
	var tokens *TokenPair
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		user.RecordSuccessfulLogin()
		if err := s.userRepo.Update(ctx, user); err != nil {
			return fmt.Errorf("record successful login: %w", err)
		}
		var genErr error
		tokens, genErr = s.generateTokenPair(ctx, user, info, id.Nil(), opts)
		return genErr
	}); err != nil {
		return nil, err
	}
	user.MFAPending = opts.MFAPending
	user.MFAVerifiedAt = opts.MFAVerifiedAt
	return &LoginResult{Status: LoginStatusComplete, Tokens: tokens, User: user}, nil
}

func (s *Service) VerifyMFA(ctx context.Context, challengeID id.ID, code string, info SessionInfo) (*LoginResult, error) {
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	var result *LoginResult
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		ch, err := s.mfaRepo.GetChallenge(ctx, challengeID)
		if err != nil {
			return err
		}
		if err := s.validateChallenge(ch, "login"); err != nil {
			return err
		}
		user, err := s.loadUserWithAuth(ctx, ch.UserID)
		if err != nil {
			return err
		}
		if err := s.verifyTOTPProof(ctx, user, code, info); err != nil {
			return err
		}
		if err := s.mfaRepo.ConsumeChallenge(ctx, challengeID); err != nil {
			return err
		}
		now := time.Now().UTC()
		var finalErr error
		result, finalErr = s.finalizeLogin(ctx, user, info, TokenSessionOptions{
			MFAVerifiedAt: &now,
			MFAMethod:     "totp",
		})
		return finalErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) StartMFAEnrollment(ctx context.Context, challengeID id.ID) (*MFAEnrollStartResult, error) {
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	var result *MFAEnrollStartResult
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		ch, err := s.mfaRepo.GetChallenge(ctx, challengeID)
		if err != nil {
			return err
		}
		if err := s.validateChallenge(ch, "enrollment"); err != nil {
			return err
		}
		user, err := s.loadUserWithAuth(ctx, ch.UserID)
		if err != nil {
			return err
		}
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      s.config.MFAIssuer,
			AccountName: user.Email,
			SecretSize:  20,
		})
		if err != nil {
			return fmt.Errorf("generate totp key: %w", err)
		}
		ciphertext, nonce, err := s.encryptMFASecret(user.ID, key.Secret())
		if err != nil {
			return err
		}
		if err := s.mfaRepo.StoreChallengeSetupSecret(ctx, challengeID, ciphertext, nonce); err != nil {
			return err
		}
		if s.qrCodeEncoder == nil {
			return apperror.NewInternal(fmt.Errorf("MFA QR encoder is not configured"))
		}
		qrDataURL, err := s.qrCodeEncoder.EncodeDataURL(key.URL())
		if err != nil {
			return fmt.Errorf("encode totp qr: %w", err)
		}
		result = &MFAEnrollStartResult{
			ChallengeID:   challengeID,
			OTPAuthURL:    key.URL(),
			QRCodeDataURL: qrDataURL,
			ManualSecret:  key.Secret(),
			ExpiresAt:     ch.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) CreateSessionEnrollmentChallenge(ctx context.Context, info SessionInfo) (*MFAChallenge, error) {
	userCtx := appctx.GetUser(ctx)
	if userCtx == nil {
		return nil, apperror.NewUnauthorized("authentication required")
	}
	userID, err := id.Parse(userCtx.UserID)
	if err != nil {
		return nil, apperror.NewUnauthorized("invalid user identity").WithCause(err)
	}
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	var ch *MFAChallenge
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		user, err := s.loadUserWithAuth(ctx, userID)
		if err != nil {
			return err
		}
		if user.MFAEnabled {
			return apperror.NewBusinessRule("MFA_ALREADY_ENABLED", "MFA is already enabled")
		}
		ch, err = s.createMFAChallenge(ctx, userID, "enrollment", info)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Service) ConfirmMFAEnrollment(ctx context.Context, challengeID id.ID, code string, info SessionInfo) (*LoginResult, error) {
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	var result *LoginResult
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		ch, err := s.mfaRepo.GetChallenge(ctx, challengeID)
		if err != nil {
			return err
		}
		if err := s.validateChallenge(ch, "enrollment"); err != nil {
			return err
		}
		if len(ch.SetupSecretCiphertext) == 0 || len(ch.SetupSecretNonce) == 0 {
			return apperror.NewValidation("MFA enrollment has not been started")
		}
		user, err := s.loadUserWithAuth(ctx, ch.UserID)
		if err != nil {
			return err
		}
		secret, err := s.decryptMFASecret(user.ID, ch.SetupSecretCiphertext, ch.SetupSecretNonce)
		if err != nil {
			return err
		}
		matchedStep, ok, err := matchTOTPCode(secret, strings.TrimSpace(code), time.Now().UTC())
		if err != nil {
			return apperror.NewMFADenied("invalid TOTP code").WithCause(err)
		}
		if !ok {
			return apperror.NewMFADenied("invalid TOTP code")
		}
		now := time.Now().UTC()
		if err := s.mfaRepo.UpsertTOTPSecret(ctx, user.ID, ch.SetupSecretCiphertext, ch.SetupSecretNonce, now); err != nil {
			return err
		}
		if err := s.mfaRepo.UpdateTOTPLastUsed(ctx, user.ID, matchedStep); err != nil {
			return err
		}
		if err := s.mfaRepo.ConsumeChallenge(ctx, challengeID); err != nil {
			return err
		}
		user.MFAEnabled = true
		result, err = s.finalizeLogin(ctx, user, info, TokenSessionOptions{
			MFAVerifiedAt: &now,
			MFAMethod:     "totp",
		})
		if err != nil {
			return err
		}
		s.emitSecurityEvent(ctx, eventlog.EventSecurityMFAEnrolled, eventlog.SeverityInfo, user.ID, user.Email, info.IPAddress,
			fmt.Sprintf("MFA enrolled for user %s", user.Email), map[string]any{"email": user.Email})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) StepUp(ctx context.Context, password, totpCode, actionCode string, info SessionInfo) (time.Time, error) {
	userCtx := appctx.GetUser(ctx)
	if userCtx == nil {
		return time.Time{}, apperror.NewUnauthorized("authentication required")
	}
	userID, err := id.Parse(userCtx.UserID)
	if err != nil {
		return time.Time{}, apperror.NewUnauthorized("invalid user identity").WithCause(err)
	}
	sessionID, err := id.Parse(userCtx.SessionID)
	if err != nil || id.IsNil(sessionID) {
		return time.Time{}, apperror.NewUnauthorized("invalid session")
	}
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return time.Time{}, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	var expiresAt time.Time
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		user, err := s.loadUserWithAuth(ctx, userID)
		if err != nil {
			return err
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			s.emitSecurityEvent(ctx, eventlog.EventSecurityStepUpDenied, eventlog.SeverityWarning, user.ID, user.Email, info.IPAddress,
				fmt.Sprintf("Step-up password denied for user %s", user.Email), map[string]any{"actionCode": actionCode})
			return apperror.NewUnauthorized("invalid credentials")
		}
		if err := s.verifyTOTPProof(ctx, user, totpCode, info); err != nil {
			s.emitSecurityEvent(ctx, eventlog.EventSecurityStepUpDenied, eventlog.SeverityWarning, user.ID, user.Email, info.IPAddress,
				fmt.Sprintf("Step-up MFA denied for user %s", user.Email), map[string]any{"actionCode": actionCode})
			return err
		}
		now := time.Now().UTC()
		if s.authStateRepo != nil {
			if err := s.authStateRepo.MarkSessionMFA(ctx, sessionID, userID, now, "totp"); err != nil {
				return err
			}
		}
		expiresAt = now.Add(s.config.AdminStepUpTTL)
		tenantID := tenant.GetTenantID(ctx)
		if err := s.mfaRepo.CreateStepUpConfirmation(ctx, sessionID, userID, tenantID, actionCode, expiresAt); err != nil {
			return err
		}
		s.emitSecurityEvent(ctx, eventlog.EventSecurityStepUpVerified, eventlog.SeverityInfo, user.ID, user.Email, info.IPAddress,
			fmt.Sprintf("Step-up verified for user %s", user.Email), map[string]any{"actionCode": actionCode, "expiresAt": expiresAt})
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	s.invalidateSessionAuthCache(ctx, sessionID)
	return expiresAt, nil
}

func (s *Service) ResetUserMFA(ctx context.Context, targetUserID id.ID, info SessionInfo) (*User, error) {
	actor := appctx.GetUser(ctx)
	if actor == nil {
		return nil, apperror.NewUnauthorized("authentication required")
	}
	if !appctx.HasRole(ctx, "security_admin") {
		return nil, apperror.NewForbidden("security_admin role is required")
	}
	actorID, err := id.Parse(actor.UserID)
	if err != nil {
		return nil, apperror.NewUnauthorized("invalid user identity").WithCause(err)
	}
	if actorID == targetUserID {
		return nil, apperror.NewForbidden("security_admin cannot reset own MFA")
	}
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	var target *User
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		var err error
		target, err = s.userRepo.GetByID(ctx, targetUserID)
		if err != nil {
			return err
		}
		if err := s.mfaRepo.ClearTOTP(ctx, targetUserID); err != nil {
			return err
		}
		if err := s.tokenRepo.RevokeAllUserTokens(ctx, targetUserID, "mfa_reset"); err != nil {
			return err
		}
		if s.authStateRepo != nil {
			if err := s.authStateRepo.RevokeAllUserSessions(ctx, targetUserID, "mfa_reset"); err != nil {
				return err
			}
			if err := s.bumpUserAuthVersion(ctx, targetUserID, "mfa_reset"); err != nil {
				return err
			}
		}
		s.emitSecurityEvent(ctx, eventlog.EventSecurityMFAReset, eventlog.SeverityCritical, targetUserID, target.Email, info.IPAddress,
			fmt.Sprintf("MFA reset for user %s", target.Email),
			map[string]any{"targetUserId": targetUserID.String(), "actorUserId": actor.UserID},
		)
		if s.notificationRepo != nil {
			link := "/settings"
			if err := s.notificationRepo.Create(ctx, &notifications.Notification{
				UserID:   target.ID,
				Title:    "MFA reset",
				Message:  "Your MFA has been reset. The next admin login will require enrollment.",
				Severity: notifications.SeverityWarning,
				Link:     &link,
				Attributes: map[string]any{
					"event":       "security.mfa_reset",
					"actorUserId": actor.UserID,
				},
			}); err != nil {
				return fmt.Errorf("create mfa reset notification: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateUserAuthCache(ctx, targetUserID)
	return target, nil
}
