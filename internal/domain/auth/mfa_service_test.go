package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain/eventlog"
)

const testTOTPSecret = "JBSWY3DPEHPK3PXP"

type testTxManager struct{}

func (testTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func newMFATestService(userRepo *fakeUserRepo, mfaRepo *fakeMFARepo, authStateRepo *fakeAuthStateRepo) *Service {
	cfg := DefaultServiceConfig()
	cfg.MFAEncryptionKey = []byte("0123456789abcdef0123456789abcdef")
	cfg.AdminStepUpTTL = 5 * time.Minute
	cfg.AdminIdleTimeout = 30 * time.Minute
	cfg.AdminAbsoluteMaxAge = 12 * time.Hour

	return NewService(
		userRepo,
		nil,
		nil,
		newFakeTokenRepo(),
		mfaRepo,
		authStateRepo,
		nil,
		testTxManager{},
		fakeAccessTokens{},
		nil,
		nil,
		cfg,
		WithQRCodeEncoder(fakeQRCode{}),
	)
}

func TestLoginAdminCIDRDeniedBeforePasswordCheck(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := NewUser("admin@example.com", string(passwordHash))
	user.IsAdmin = true
	repo := newFakeUserRepo(user)
	repo.rolesByUser[user.ID] = []Role{{Code: "admin"}}
	mfaRepo := newFakeMFARepo()
	svc := newMFATestService(repo, mfaRepo, &fakeAuthStateRepo{})
	svc.config.AdminSecurityMode = AdminSecurityEnforce
	svc.config.AdminMFAMode = MFAModeRequired

	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: "tenant-a"})
	_, err = svc.Login(ctx, Credentials{Email: user.Email, Password: "wrong-password"}, SessionInfo{IPAddress: "198.51.100.10"})
	appErr := requireAppError(t, err)
	if appErr.Code != apperror.CodeAdminIPDenied {
		t.Fatalf("expected %s before password check, got %s", apperror.CodeAdminIPDenied, appErr.Code)
	}
	if user.FailedLoginAttempts != 0 {
		t.Fatalf("password path should not run after CIDR deny, failed attempts=%d", user.FailedLoginAttempts)
	}
	if len(mfaRepo.challenges) != 0 {
		t.Fatalf("CIDR deny must not create MFA challenges, got %d", len(mfaRepo.challenges))
	}
}

func TestLoginPasswordDeniedDoesNotCreateMFAChallenge(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := NewUser("admin@example.com", string(passwordHash))
	user.IsAdmin = true
	repo := newFakeUserRepo(user)
	repo.rolesByUser[user.ID] = []Role{{Code: "admin"}}
	mfaRepo := newFakeMFARepo()
	svc := newMFATestService(repo, mfaRepo, &fakeAuthStateRepo{})
	svc.config.AdminMFAMode = MFAModeRequired

	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: "tenant-a"})
	_, err = svc.Login(ctx, Credentials{Email: user.Email, Password: "wrong-password"}, SessionInfo{IPAddress: "127.0.0.1"})
	appErr := requireAppError(t, err)
	if appErr.Code != apperror.CodeUnauthorized {
		t.Fatalf("expected %s, got %s", apperror.CodeUnauthorized, appErr.Code)
	}
	if user.FailedLoginAttempts != 1 {
		t.Fatalf("expected one failed login attempt, got %d", user.FailedLoginAttempts)
	}
	if len(mfaRepo.challenges) != 0 {
		t.Fatalf("password denial must not create MFA challenges, got %d", len(mfaRepo.challenges))
	}
}

func TestLoginRequiredModeWithActiveTOTPReturnsChallengeWithoutSession(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := NewUser("admin@example.com", string(passwordHash))
	user.IsAdmin = true
	repo := newFakeUserRepo(user)
	repo.rolesByUser[user.ID] = []Role{{Code: "admin"}}
	mfaRepo := newFakeMFARepo()
	authStateRepo := &fakeAuthStateRepo{}
	svc := newMFATestService(repo, mfaRepo, authStateRepo)
	svc.config.AdminMFAMode = MFAModeRequired
	mfaRepo.totp[user.ID] = &TOTPRecord{UserID: user.ID, Enabled: true}

	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: "tenant-a"})
	result, err := svc.Login(ctx, Credentials{Email: user.Email, Password: "correct-password"}, SessionInfo{IPAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("login should return MFA challenge: %v", err)
	}
	if result.Status != LoginStatusMFARequired {
		t.Fatalf("expected %s, got %s", LoginStatusMFARequired, result.Status)
	}
	if result.Tokens != nil {
		t.Fatal("MFA challenge must not issue final tokens")
	}
	if id.IsNil(result.ChallengeID) {
		t.Fatal("expected challenge ID")
	}
	if len(authStateRepo.createdSessions) != 0 {
		t.Fatalf("MFA challenge must not create final auth session, got %d", len(authStateRepo.createdSessions))
	}
	if len(mfaRepo.challenges) != 1 {
		t.Fatalf("expected one MFA challenge, got %d", len(mfaRepo.challenges))
	}
}

func TestLoginRequiredModeExpiredGraceReturnsEnrollmentChallenge(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := NewUser("admin@example.com", string(passwordHash))
	user.IsAdmin = true
	repo := newFakeUserRepo(user)
	repo.rolesByUser[user.ID] = []Role{{Code: "admin"}}
	mfaRepo := newFakeMFARepo()
	authStateRepo := &fakeAuthStateRepo{}
	svc := newMFATestService(repo, mfaRepo, authStateRepo)
	svc.config.AdminMFAMode = MFAModeRequired
	requiredAfter := time.Now().UTC().Add(-time.Minute)
	mfaRepo.totp[user.ID] = &TOTPRecord{UserID: user.ID, RequiredAfter: &requiredAfter}

	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: "tenant-a"})
	result, err := svc.Login(ctx, Credentials{Email: user.Email, Password: "correct-password"}, SessionInfo{IPAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("login should return enrollment challenge: %v", err)
	}
	if result.Status != LoginStatusMFAEnrollmentRequired {
		t.Fatalf("expected %s, got %s", LoginStatusMFAEnrollmentRequired, result.Status)
	}
	if result.Tokens != nil {
		t.Fatal("enrollment challenge after grace must not issue final tokens")
	}
	if id.IsNil(result.ChallengeID) {
		t.Fatal("expected challenge ID")
	}
	if len(authStateRepo.createdSessions) != 0 {
		t.Fatalf("enrollment challenge must not create final auth session, got %d", len(authStateRepo.createdSessions))
	}
}

func TestLoginOptionalModeUnenrolledCreatesPendingSession(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := NewUser("admin@example.com", string(passwordHash))
	user.IsAdmin = true
	repo := newFakeUserRepo(user)
	repo.rolesByUser[user.ID] = []Role{{Code: "admin"}}
	mfaRepo := newFakeMFARepo()
	authStateRepo := &fakeAuthStateRepo{}
	svc := newMFATestService(repo, mfaRepo, authStateRepo)
	svc.config.AdminMFAMode = MFAModeOptional

	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: "tenant-a"})
	result, err := svc.Login(ctx, Credentials{Email: user.Email, Password: "correct-password"}, SessionInfo{IPAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("optional login should complete with pending MFA: %v", err)
	}
	if result.Status != LoginStatusComplete {
		t.Fatalf("expected %s, got %s", LoginStatusComplete, result.Status)
	}
	if result.Tokens == nil {
		t.Fatal("optional unenrolled login should issue final tokens")
	}
	if len(authStateRepo.createdSessions) != 1 {
		t.Fatalf("expected one auth session, got %d", len(authStateRepo.createdSessions))
	}
	if !authStateRepo.createdSessions[0].MFAPending {
		t.Fatal("expected pending MFA marker on optional unenrolled admin session")
	}
	session := authStateRepo.createdSessions[0]
	expectedScope := CacheScope{TenantID: "tenant-a", PrincipalID: user.ID.String(), SessionID: session.ID.String(),
		UserVersion: fmt.Sprint(session.UserAuthVersion), PolicyVersion: fmt.Sprint(session.PolicyVersion)}
	if result.Tokens.CacheScope != expectedScope {
		t.Fatalf("client cache scope must match server session: got %+v, want %+v", result.Tokens.CacheScope, expectedScope)
	}
	rotated, err := svc.generateTokenPair(ctx, user, SessionInfo{}, session.ID, TokenSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rotated.CacheScope != expectedScope {
		t.Fatal("token rotation in the same session must preserve the cache scope")
	}
}

func TestVerifyTOTPProofNotEnrolledUsesTypedReason(t *testing.T) {
	user := NewUser("admin@example.com", "hash")
	svc := newMFATestService(newFakeUserRepo(user), newFakeMFARepo(), &fakeAuthStateRepo{})

	err := svc.verifyTOTPProof(context.Background(), user, "123456", SessionInfo{})
	appErr := requireAppError(t, err)

	if appErr.Code != apperror.CodeMFADenied {
		t.Fatalf("expected %s, got %s", apperror.CodeMFADenied, appErr.Code)
	}
	if appErr.Details["reason"] != apperror.ReasonMFANotEnrolled {
		t.Fatalf("expected reason %s, got %#v", apperror.ReasonMFANotEnrolled, appErr.Details["reason"])
	}
}

func TestVerifyTOTPProofRejectsReplayOfSameTimeStep(t *testing.T) {
	user := NewUser("admin@example.com", "hash")
	mfaRepo := newFakeMFARepo()
	svc := newMFATestService(newFakeUserRepo(user), mfaRepo, &fakeAuthStateRepo{})
	enrollTestTOTP(t, svc, mfaRepo, user.ID, testTOTPSecret)

	code := generateTestTOTPCode(t, testTOTPSecret, time.Now().UTC())
	if err := svc.verifyTOTPProof(context.Background(), user, code, SessionInfo{}); err != nil {
		t.Fatalf("first TOTP proof should pass: %v", err)
	}

	err := svc.verifyTOTPProof(context.Background(), user, code, SessionInfo{})
	appErr := requireAppError(t, err)
	if appErr.Code != apperror.CodeMFADenied {
		t.Fatalf("expected replay to return %s, got %s", apperror.CodeMFADenied, appErr.Code)
	}
}

func TestVerifyTOTPProofLocksAfterFiveFailures(t *testing.T) {
	user := NewUser("admin@example.com", "hash")
	mfaRepo := newFakeMFARepo()
	svc := newMFATestService(newFakeUserRepo(user), mfaRepo, &fakeAuthStateRepo{})
	enrollTestTOTP(t, svc, mfaRepo, user.ID, testTOTPSecret)

	var err error
	for range 5 {
		err = svc.verifyTOTPProof(context.Background(), user, "000000", SessionInfo{})
	}
	appErr := requireAppError(t, err)
	if appErr.Code != apperror.CodeMFALocked {
		t.Fatalf("expected %s after five failures, got %s", apperror.CodeMFALocked, appErr.Code)
	}
	if rec := mfaRepo.totp[user.ID]; rec.LockedUntil == nil || !rec.LockedUntil.After(time.Now().UTC()) {
		t.Fatalf("expected locked_until in the future, got %#v", rec.LockedUntil)
	}
}

func TestStepUpMarksSessionMFAAndCreatesConfirmation(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := NewUser("admin@example.com", string(passwordHash))
	sessionID := id.New()
	mfaRepo := newFakeMFARepo()
	authStateRepo := &fakeAuthStateRepo{}
	svc := newMFATestService(newFakeUserRepo(user), mfaRepo, authStateRepo)
	enrollTestTOTP(t, svc, mfaRepo, user.ID, testTOTPSecret)

	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: "tenant-a"})
	ctx = appctx.WithUser(ctx, &appctx.UserContext{
		UserID:    user.ID.String(),
		SessionID: sessionID.String(),
		Email:     user.Email,
	})

	code := generateTestTOTPCode(t, testTOTPSecret, time.Now().UTC())
	expiresAt, err := svc.StepUp(ctx, "correct-password", code, "system.settings.update", SessionInfo{IPAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("step-up should pass: %v", err)
	}
	if !expiresAt.After(time.Now().UTC()) {
		t.Fatalf("expected future step-up expiry, got %s", expiresAt)
	}
	if authStateRepo.markedSessionID != sessionID || authStateRepo.markedUserID != user.ID || authStateRepo.markedMethod != "totp" {
		t.Fatalf("session MFA marker not recorded: %#v", authStateRepo)
	}
	ok, err := mfaRepo.HasFreshStepUp(ctx, sessionID, user.ID, "tenant-a", "system.settings.update", time.Now().UTC())
	if err != nil {
		t.Fatalf("check step-up: %v", err)
	}
	if !ok {
		t.Fatal("expected fresh step-up confirmation")
	}
}

func enrollTestTOTP(t *testing.T, svc *Service, repo *fakeMFARepo, userID id.ID, secret string) {
	t.Helper()
	ciphertext, nonce, err := svc.encryptMFASecret(userID, secret)
	if err != nil {
		t.Fatalf("encrypt TOTP secret: %v", err)
	}
	now := time.Now().UTC()
	repo.totp[userID] = &TOTPRecord{
		UserID:           userID,
		SecretCiphertext: ciphertext,
		SecretNonce:      nonce,
		Enabled:          true,
		EnrolledAt:       &now,
	}
}

func generateTestTOTPCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period:    _totpPeriodSeconds,
		Skew:      0,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	return code
}

func requireAppError(t *testing.T, err error) *apperror.AppError {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	appErr, ok := apperror.AsAppError(err)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	return appErr
}

type fakeUserRepo struct {
	users       map[id.ID]*User
	rolesByUser map[id.ID][]Role
	permsByUser map[id.ID][]string
}

func newFakeUserRepo(users ...*User) *fakeUserRepo {
	repo := &fakeUserRepo{
		users:       map[id.ID]*User{},
		rolesByUser: map[id.ID][]Role{},
		permsByUser: map[id.ID][]string{},
	}
	for _, user := range users {
		repo.users[user.ID] = user
	}
	return repo
}

func (r *fakeUserRepo) Create(ctx context.Context, user *User) error {
	r.users[user.ID] = user
	return nil
}

func (r *fakeUserRepo) GetByID(ctx context.Context, userID id.ID) (*User, error) {
	user, ok := r.users[userID]
	if !ok {
		return nil, apperror.NewNotFound("users", userID.String())
	}
	return user, nil
}

func (r *fakeUserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	for _, user := range r.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, apperror.NewNotFound("users", email)
}

func (r *fakeUserRepo) Update(ctx context.Context, user *User) error {
	r.users[user.ID] = user
	return nil
}

func (r *fakeUserRepo) Delete(ctx context.Context, userID id.ID) error {
	delete(r.users, userID)
	return nil
}

func (r *fakeUserRepo) List(ctx context.Context, filter UserFilter) ([]User, int, error) {
	items := make([]User, 0, len(r.users))
	for _, user := range r.users {
		items = append(items, *user)
	}
	return items, len(items), nil
}

func (r *fakeUserRepo) LoadRoles(ctx context.Context, userID id.ID) ([]Role, error) {
	return append([]Role(nil), r.rolesByUser[userID]...), nil
}

func (r *fakeUserRepo) LoadRolesForUsers(ctx context.Context, userIDs []id.ID) (map[id.ID][]Role, error) {
	roles := make(map[id.ID][]Role, len(userIDs))
	for _, userID := range userIDs {
		roles[userID] = append([]Role(nil), r.rolesByUser[userID]...)
	}
	return roles, nil
}

func (r *fakeUserRepo) LoadPermissions(ctx context.Context, userID id.ID) ([]string, error) {
	return append([]string(nil), r.permsByUser[userID]...), nil
}

func (r *fakeUserRepo) AssignRole(ctx context.Context, userID, roleID id.ID, grantedBy id.ID) error {
	return nil
}

func (r *fakeUserRepo) RevokeRole(ctx context.Context, userID, roleID id.ID) error {
	return nil
}

func (r *fakeUserRepo) Exists(ctx context.Context, email string) (bool, error) {
	for _, user := range r.users {
		if user.Email == email {
			return true, nil
		}
	}
	return false, nil
}

type fakeMFARepo struct {
	totp          map[id.ID]*TOTPRecord
	challenges    map[id.ID]*MFAChallenge
	confirmations map[string]time.Time
}

func newFakeMFARepo() *fakeMFARepo {
	return &fakeMFARepo{
		totp:          map[id.ID]*TOTPRecord{},
		challenges:    map[id.ID]*MFAChallenge{},
		confirmations: map[string]time.Time{},
	}
}

func (r *fakeMFARepo) GetTOTP(ctx context.Context, userID id.ID) (*TOTPRecord, error) {
	rec, ok := r.totp[userID]
	if !ok {
		return nil, apperror.NewNotFound("user_mfa_totp", userID.String())
	}
	return rec, nil
}

func (r *fakeMFARepo) LoadMFAStatuses(ctx context.Context, userIDs []id.ID) (map[id.ID]MFAStatus, error) {
	statuses := make(map[id.ID]MFAStatus, len(userIDs))
	for _, userID := range userIDs {
		if rec := r.totp[userID]; rec != nil {
			statuses[userID] = MFAStatus{Enabled: rec.Enabled, RequiredAfter: rec.RequiredAfter}
		}
	}
	return statuses, nil
}

func (r *fakeMFARepo) EnsureTOTPRequiredAfter(ctx context.Context, userID id.ID, requiredAfter time.Time) (*TOTPRecord, error) {
	rec, ok := r.totp[userID]
	if !ok {
		rec = &TOTPRecord{UserID: userID}
		r.totp[userID] = rec
	}
	if rec.RequiredAfter == nil {
		rec.RequiredAfter = &requiredAfter
	}
	return rec, nil
}

func (r *fakeMFARepo) UpsertTOTPSecret(ctx context.Context, userID id.ID, ciphertext, nonce []byte, enrolledAt time.Time) error {
	r.totp[userID] = &TOTPRecord{
		UserID:           userID,
		SecretCiphertext: ciphertext,
		SecretNonce:      nonce,
		Enabled:          true,
		EnrolledAt:       &enrolledAt,
	}
	return nil
}

func (r *fakeMFARepo) ClearTOTP(ctx context.Context, userID id.ID) error {
	delete(r.totp, userID)
	return nil
}

func (r *fakeMFARepo) UpdateTOTPLastUsed(ctx context.Context, userID id.ID, timeStep int64) error {
	rec, ok := r.totp[userID]
	if !ok || !rec.Enabled {
		return apperror.NewMFANotEnrolled()
	}
	if rec.LastUsedTimeStep != nil && *rec.LastUsedTimeStep >= timeStep {
		return apperror.NewMFADenied("TOTP code has already been used")
	}
	rec.LastUsedTimeStep = &timeStep
	return nil
}

func (r *fakeMFARepo) RecordTOTPFailure(ctx context.Context, userID id.ID, now time.Time, maxAttempts int, window, lockDuration time.Duration) (*TOTPRecord, bool, error) {
	rec, ok := r.totp[userID]
	if !ok {
		return nil, false, apperror.NewNotFound("user_mfa_totp", userID.String())
	}
	attempts := 1
	windowStart := now
	if rec.FailedWindowStart != nil && now.Sub(*rec.FailedWindowStart) <= window {
		attempts = rec.FailedAttempts + 1
		windowStart = *rec.FailedWindowStart
	}
	rec.FailedAttempts = attempts
	rec.FailedWindowStart = &windowStart
	locked := attempts >= maxAttempts
	if locked {
		lockedUntil := now.Add(lockDuration)
		rec.LockedUntil = &lockedUntil
	}
	return rec, locked, nil
}

func (r *fakeMFARepo) ResetTOTPFailures(ctx context.Context, userID id.ID) error {
	if rec := r.totp[userID]; rec != nil {
		rec.FailedAttempts = 0
		rec.FailedWindowStart = nil
		rec.LockedUntil = nil
	}
	return nil
}

func (r *fakeMFARepo) CreateChallenge(ctx context.Context, challenge *MFAChallenge) error {
	if id.IsNil(challenge.ID) {
		challenge.ID = id.New()
	}
	r.challenges[challenge.ID] = challenge
	return nil
}

func (r *fakeMFARepo) GetChallenge(ctx context.Context, challengeID id.ID) (*MFAChallenge, error) {
	ch, ok := r.challenges[challengeID]
	if !ok {
		return nil, apperror.NewNotFound("auth_mfa_challenge", challengeID.String())
	}
	return ch, nil
}

func (r *fakeMFARepo) StoreChallengeSetupSecret(ctx context.Context, challengeID id.ID, ciphertext, nonce []byte) error {
	ch, ok := r.challenges[challengeID]
	if !ok || ch.ConsumedAt != nil || !time.Now().UTC().Before(ch.ExpiresAt) {
		return apperror.NewMFADenied("MFA challenge is invalid or expired")
	}
	ch.SetupSecretCiphertext = ciphertext
	ch.SetupSecretNonce = nonce
	return nil
}

func (r *fakeMFARepo) ConsumeChallenge(ctx context.Context, challengeID id.ID) error {
	ch, ok := r.challenges[challengeID]
	if !ok || ch.ConsumedAt != nil || !time.Now().UTC().Before(ch.ExpiresAt) {
		return apperror.NewMFADenied("MFA challenge is invalid or expired")
	}
	now := time.Now().UTC()
	ch.ConsumedAt = &now
	return nil
}

func (r *fakeMFARepo) CreateStepUpConfirmation(ctx context.Context, sessionID, userID id.ID, tenantID, actionCode string, expiresAt time.Time) error {
	r.confirmations[stepUpKey(sessionID, userID, tenantID, actionCode)] = expiresAt
	return nil
}

func (r *fakeMFARepo) HasFreshStepUp(ctx context.Context, sessionID, userID id.ID, tenantID, actionCode string, now time.Time) (bool, error) {
	expiresAt, ok := r.confirmations[stepUpKey(sessionID, userID, tenantID, actionCode)]
	return ok && expiresAt.After(now), nil
}

func (r *fakeMFARepo) DeleteExpiredStepUps(ctx context.Context) error {
	now := time.Now().UTC()
	for key, expiresAt := range r.confirmations {
		if !expiresAt.After(now) {
			delete(r.confirmations, key)
		}
	}
	return nil
}

func stepUpKey(sessionID, userID id.ID, tenantID, actionCode string) string {
	return fmt.Sprintf("%s:%s:%s:%s", sessionID, userID, tenantID, actionCode)
}

type fakeTokenRepo struct {
	byID   map[id.ID]*RefreshToken
	byHash map[string]*RefreshToken
}

func newFakeTokenRepo() *fakeTokenRepo {
	return &fakeTokenRepo{
		byID:   map[id.ID]*RefreshToken{},
		byHash: map[string]*RefreshToken{},
	}
}

func (r *fakeTokenRepo) SaveRefreshToken(ctx context.Context, token *RefreshToken) error {
	r.byID[token.ID] = token
	r.byHash[token.TokenHash] = token
	return nil
}

func (r *fakeTokenRepo) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	token, ok := r.byHash[tokenHash]
	if !ok {
		return nil, apperror.NewNotFound("refresh_tokens", tokenHash)
	}
	return token, nil
}

func (r *fakeTokenRepo) RevokeRefreshToken(ctx context.Context, tokenID id.ID, reason string) error {
	token, ok := r.byID[tokenID]
	if !ok {
		return apperror.NewNotFound("refresh_tokens", tokenID.String())
	}
	now := time.Now().UTC()
	reasonCopy := reason
	token.RevokedAt = &now
	token.RevokedReason = &reasonCopy
	return nil
}

func (r *fakeTokenRepo) RevokeAllUserTokens(ctx context.Context, userID id.ID, reason string) error {
	now := time.Now().UTC()
	reasonCopy := reason
	for _, token := range r.byID {
		if token.UserID == userID {
			token.RevokedAt = &now
			token.RevokedReason = &reasonCopy
		}
	}
	return nil
}

func (r *fakeTokenRepo) CleanupExpiredTokens(ctx context.Context) (int, error) {
	return 0, nil
}

type fakeAuthStateRepo struct {
	markedSessionID id.ID
	markedUserID    id.ID
	markedMethod    string
	markedAt        time.Time
	createdSessions []*AuthSession
}

func (r *fakeAuthStateRepo) CreateSession(ctx context.Context, session *AuthSession) error {
	r.createdSessions = append(r.createdSessions, session)
	return nil
}

func (r *fakeAuthStateRepo) GetSessionState(ctx context.Context, userID, sessionID id.ID) (*AuthSessionState, error) {
	return &AuthSessionState{SessionID: sessionID, UserID: userID, UserAuthVersion: 1, PolicyVersion: 1}, nil
}

func (r *fakeAuthStateRepo) RevokeSession(ctx context.Context, sessionID id.ID, reason string) error {
	return nil
}

func (r *fakeAuthStateRepo) RevokeAllUserSessions(ctx context.Context, userID id.ID, reason string) error {
	return nil
}

func (r *fakeAuthStateRepo) ExtendSession(ctx context.Context, sessionID id.ID, expiresAt time.Time, info SessionInfo) error {
	return nil
}

func (r *fakeAuthStateRepo) MarkSessionMFA(ctx context.Context, sessionID, userID id.ID, verifiedAt time.Time, method string) error {
	r.markedSessionID = sessionID
	r.markedUserID = userID
	r.markedAt = verifiedAt
	r.markedMethod = method
	return nil
}

func (r *fakeAuthStateRepo) BumpUserAuthVersion(ctx context.Context, userID id.ID) (int64, error) {
	return 1, nil
}

func (r *fakeAuthStateRepo) GetCurrentPolicyVersion(ctx context.Context) (int64, error) {
	return 1, nil
}

func (r *fakeAuthStateRepo) BumpPolicyVersion(ctx context.Context) (int64, error) {
	return 1, nil
}

// These tests exercise enrollment and session policy independently of token/PNG formats.
type fakeAccessTokens struct{}

func (fakeAccessTokens) GenerateAccessToken(string, string, string, string, int64, int64, []string, []string, bool, appctx.AccessScopes) (string, time.Time, error) {
	return "test-access-token", time.Now().Add(time.Minute), nil
}

type fakeQRCode struct{}

func (fakeQRCode) EncodeDataURL(string) (string, error) { return "data:image/png;base64,test", nil }

type rollbackTxManager struct {
	userRepo *fakeUserRepo
	mfaRepo  *fakeMFARepo
	events   *rollbackEventWriter
}

func (m *rollbackTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	// Snapshot mutable state before running transactional work so any non-nil
	// error returned from fn rolls back mutations made inside the transaction,
	// matching real PostgreSQL tx semantics.
	totpBackup := map[id.ID]TOTPRecord{}
	for k, v := range m.mfaRepo.totp {
		if v != nil {
			totpBackup[k] = *v
		}
	}
	userBackup := map[id.ID]User{}
	for k, v := range m.userRepo.users {
		if v != nil {
			userBackup[k] = *v
		}
	}
	eventsLen := len(m.events.events)

	err := fn(ctx)
	if err != nil {
		for k, snap := range totpBackup {
			copyRec := snap
			m.mfaRepo.totp[k] = &copyRec
		}
		for k, snap := range userBackup {
			copyUser := snap
			m.userRepo.users[k] = &copyUser
		}
		m.events.events = m.events.events[:eventsLen]
	}
	return err
}

type rollbackEventWriter struct {
	events []eventlog.EventType
}

func (w *rollbackEventWriter) Write(_ context.Context, ev eventlog.Event) error {
	w.events = append(w.events, ev.EventType)
	return nil
}

func (w *rollbackEventWriter) WriteBatch(_ context.Context, _ []eventlog.Event) error {
	return nil
}

func TestVerifyMFAAndStepUpPersistFailuresAcrossRollback(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := NewUser("admin@example.com", string(passwordHash))
	user.IsAdmin = true
	user.EmailVerified = true

	userRepo := newFakeUserRepo(user)
	mfaRepo := newFakeMFARepo()
	events := &rollbackEventWriter{}
	txm := &rollbackTxManager{userRepo: userRepo, mfaRepo: mfaRepo, events: events}

	cfg := DefaultServiceConfig()
	cfg.MFAEncryptionKey = []byte("0123456789abcdef0123456789abcdef")
	cfg.AdminStepUpTTL = 5 * time.Minute
	svc := NewService(
		userRepo, nil, nil, newFakeTokenRepo(), mfaRepo, &fakeAuthStateRepo{},
		nil, txm, fakeAccessTokens{}, events, nil, cfg,
		WithQRCodeEncoder(fakeQRCode{}),
	)
	enrollTestTOTP(t, svc, mfaRepo, user.ID, testTOTPSecret)

	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: "tenant-a"})
	ch, err := svc.createMFAChallenge(ctx, user.ID, "login", SessionInfo{IPAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	// 5 wrong TOTP attempts in VerifyMFA must persist failed_attempts, lock MFA, and retain security events.
	var verifyErr error
	for range 5 {
		_, verifyErr = svc.VerifyMFA(ctx, ch.ID, "000000", SessionInfo{IPAddress: "127.0.0.1"})
	}
	appErr := requireAppError(t, verifyErr)
	if appErr.Code != apperror.CodeMFALocked {
		t.Fatalf("expected %s after 5 wrong VerifyMFA attempts, got %s", apperror.CodeMFALocked, appErr.Code)
	}
	if got := mfaRepo.totp[user.ID].FailedAttempts; got != 5 {
		t.Fatalf("expected 5 persisted failed_attempts, got %d", got)
	}
	if len(events.events) != 5 {
		t.Fatalf("expected 5 persisted security events, got %d", len(events.events))
	}

	// Wrong password in StepUp must also persist failed login counter and security event.
	events.events = nil
	sessionID := id.New()
	stepCtx := appctx.WithUser(ctx, &appctx.UserContext{
		UserID:    user.ID.String(),
		SessionID: sessionID.String(),
		TenantID:  "tenant-a",
	})
	_, stepErr := svc.StepUp(stepCtx, "wrong-password", "000000", "auth.roles.update", SessionInfo{IPAddress: "127.0.0.1"})
	if requireAppError(t, stepErr).Code != apperror.CodeUnauthorized {
		t.Fatalf("expected unauthorized on wrong step-up password, got %v", stepErr)
	}
	if got := userRepo.users[user.ID].FailedLoginAttempts; got != 1 {
		t.Fatalf("expected persisted FailedLoginAttempts=1, got %d", got)
	}
	if len(events.events) != 1 || events.events[0] != eventlog.EventSecurityStepUpDenied {
		t.Fatalf("expected persisted step-up denied event, got %v", events.events)
	}
}
