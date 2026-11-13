package auth_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/storage/postgres"
)

// MFARepo implements auth.MFARepository.
type MFARepo struct{}

func NewMFARepo() *MFARepo {
	return &MFARepo{}
}

func (r *MFARepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// LoadMFAStatuses deliberately excludes TOTP secrets and verification state.
func (r *MFARepo) LoadMFAStatuses(ctx context.Context, userIDs []id.ID) (map[id.ID]auth.MFAStatus, error) {
	result := make(map[id.ID]auth.MFAStatus, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	q := r.getTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `SELECT user_id, enabled, required_after FROM auth_user_mfa_totp WHERE user_id = ANY($1)`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("query MFA statuses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID id.ID
		var status auth.MFAStatus
		if err := rows.Scan(&userID, &status.Enabled, &status.RequiredAfter); err != nil {
			return nil, fmt.Errorf("scan MFA status: %w", err)
		}
		result[userID] = status
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read MFA statuses: %w", err)
	}
	return result, nil
}

func (r *MFARepo) GetTOTP(ctx context.Context, userID id.ID) (*auth.TOTPRecord, error) {
	return r.getTOTP(ctx, userID, false)
}

func (r *MFARepo) getTOTP(ctx context.Context, userID id.ID, forUpdate bool) (*auth.TOTPRecord, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	query := `
		SELECT user_id, secret_ciphertext, secret_nonce, enabled, enrolled_at, disabled_at,
		       required_after, last_used_time_step, failed_attempts, failed_window_start,
		       locked_until, created_at, updated_at
		FROM auth_user_mfa_totp
		WHERE user_id = $1
	`
	if forUpdate {
		query += ` FOR UPDATE`
	}

	var rec auth.TOTPRecord
	err := q.QueryRow(ctx, query, userID).Scan(
		&rec.UserID, &rec.SecretCiphertext, &rec.SecretNonce, &rec.Enabled, &rec.EnrolledAt, &rec.DisabledAt,
		&rec.RequiredAfter, &rec.LastUsedTimeStep, &rec.FailedAttempts, &rec.FailedWindowStart,
		&rec.LockedUntil, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("user_mfa_totp", userID.String())
	}
	if err != nil {
		return nil, fmt.Errorf("get totp mfa: %w", err)
	}
	return &rec, nil
}

func (r *MFARepo) EnsureTOTPRequiredAfter(ctx context.Context, userID id.ID, requiredAfter time.Time) (*auth.TOTPRecord, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		INSERT INTO auth_user_mfa_totp (user_id, required_after)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET required_after = COALESCE(auth_user_mfa_totp.required_after, EXCLUDED.required_after)
		RETURNING user_id, secret_ciphertext, secret_nonce, enabled, enrolled_at, disabled_at,
		          required_after, last_used_time_step, failed_attempts, failed_window_start,
		          locked_until, created_at, updated_at
	`
	var rec auth.TOTPRecord
	err := q.QueryRow(ctx, query, userID, requiredAfter).Scan(
		&rec.UserID, &rec.SecretCiphertext, &rec.SecretNonce, &rec.Enabled, &rec.EnrolledAt, &rec.DisabledAt,
		&rec.RequiredAfter, &rec.LastUsedTimeStep, &rec.FailedAttempts, &rec.FailedWindowStart,
		&rec.LockedUntil, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("ensure totp required_after: %w", err)
	}
	return &rec, nil
}

func (r *MFARepo) UpsertTOTPSecret(ctx context.Context, userID id.ID, ciphertext, nonce []byte, enrolledAt time.Time) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		INSERT INTO auth_user_mfa_totp (
			user_id, secret_ciphertext, secret_nonce, enabled, enrolled_at,
			disabled_at, failed_attempts, failed_window_start, locked_until, last_used_time_step
		) VALUES ($1, $2, $3, TRUE, $4, NULL, 0, NULL, NULL, NULL)
		ON CONFLICT (user_id) DO UPDATE
		SET secret_ciphertext = EXCLUDED.secret_ciphertext,
		    secret_nonce = EXCLUDED.secret_nonce,
		    enabled = TRUE,
		    enrolled_at = EXCLUDED.enrolled_at,
		    disabled_at = NULL,
		    failed_attempts = 0,
		    failed_window_start = NULL,
		    locked_until = NULL,
		    last_used_time_step = NULL
	`
	if _, err := q.Exec(ctx, query, userID, ciphertext, nonce, enrolledAt); err != nil {
		return fmt.Errorf("upsert totp secret: %w", err)
	}
	return nil
}

func (r *MFARepo) ClearTOTP(ctx context.Context, userID id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		UPDATE auth_user_mfa_totp
		SET enabled = FALSE,
		    secret_ciphertext = NULL,
		    secret_nonce = NULL,
		    disabled_at = now(),
		    enrolled_at = NULL,
		    last_used_time_step = NULL,
		    failed_attempts = 0,
		    failed_window_start = NULL,
		    locked_until = NULL,
		    required_after = now()
		WHERE user_id = $1
	`
	if _, err := q.Exec(ctx, query, userID); err != nil {
		return fmt.Errorf("clear totp: %w", err)
	}
	return nil
}

func (r *MFARepo) UpdateTOTPLastUsed(ctx context.Context, userID id.ID, timeStep int64) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		UPDATE auth_user_mfa_totp
		SET last_used_time_step = $2
		WHERE user_id = $1
		  AND enabled = TRUE
		  AND (last_used_time_step IS NULL OR last_used_time_step < $2)
	`
	tag, err := q.Exec(ctx, query, userID, timeStep)
	if err != nil {
		return fmt.Errorf("update totp last used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewMFADenied("TOTP code has already been used")
	}
	return nil
}

func (r *MFARepo) RecordTOTPFailure(ctx context.Context, userID id.ID, now time.Time, maxAttempts int, window, lockDuration time.Duration) (*auth.TOTPRecord, bool, error) {
	rec, err := r.getTOTP(ctx, userID, true)
	if err != nil {
		return nil, false, err
	}

	attempts := 1
	windowStart := now
	if rec.FailedWindowStart != nil && now.Sub(*rec.FailedWindowStart) <= window {
		attempts = rec.FailedAttempts + 1
		windowStart = *rec.FailedWindowStart
	}

	var lockedUntil *time.Time
	locked := attempts >= maxAttempts
	if locked {
		t := now.Add(lockDuration)
		lockedUntil = &t
	}

	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		UPDATE auth_user_mfa_totp
		SET failed_attempts = $2,
		    failed_window_start = $3,
		    locked_until = $4
		WHERE user_id = $1
	`
	if _, err := q.Exec(ctx, query, userID, attempts, windowStart, lockedUntil); err != nil {
		return nil, false, fmt.Errorf("record totp failure: %w", err)
	}
	rec.FailedAttempts = attempts
	rec.FailedWindowStart = &windowStart
	rec.LockedUntil = lockedUntil
	return rec, locked, nil
}

func (r *MFARepo) ResetTOTPFailures(ctx context.Context, userID id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		UPDATE auth_user_mfa_totp
		SET failed_attempts = 0,
		    failed_window_start = NULL,
		    locked_until = NULL
		WHERE user_id = $1
	`
	if _, err := q.Exec(ctx, query, userID); err != nil {
		return fmt.Errorf("reset totp failures: %w", err)
	}
	return nil
}

func (r *MFARepo) CreateChallenge(ctx context.Context, challenge *auth.MFAChallenge) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	if id.IsNil(challenge.ID) {
		challenge.ID = id.New()
	}
	if challenge.CreatedAt.IsZero() {
		challenge.CreatedAt = time.Now().UTC()
	}
	const query = `
		INSERT INTO auth_mfa_challenges (
			id, user_id, challenge_type, setup_secret_ciphertext, setup_secret_nonce,
			expires_at, created_at, user_agent, ip_address
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::inet)
	`
	if _, err := q.Exec(ctx, query,
		challenge.ID, challenge.UserID, challenge.ChallengeType, challenge.SetupSecretCiphertext,
		challenge.SetupSecretNonce, challenge.ExpiresAt, challenge.CreatedAt, challenge.UserAgent, challenge.IPAddress,
	); err != nil {
		return fmt.Errorf("create mfa challenge: %w", err)
	}
	return nil
}

func (r *MFARepo) GetChallenge(ctx context.Context, challengeID id.ID) (*auth.MFAChallenge, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		SELECT id, user_id, challenge_type, setup_secret_ciphertext, setup_secret_nonce,
		       expires_at, consumed_at, created_at, user_agent, COALESCE(ip_address::text, '')
		FROM auth_mfa_challenges
		WHERE id = $1
		FOR UPDATE
	`
	var ch auth.MFAChallenge
	err := q.QueryRow(ctx, query, challengeID).Scan(
		&ch.ID, &ch.UserID, &ch.ChallengeType, &ch.SetupSecretCiphertext, &ch.SetupSecretNonce,
		&ch.ExpiresAt, &ch.ConsumedAt, &ch.CreatedAt, &ch.UserAgent, &ch.IPAddress,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("auth_mfa_challenge", challengeID.String())
	}
	if err != nil {
		return nil, fmt.Errorf("get mfa challenge: %w", err)
	}
	return &ch, nil
}

func (r *MFARepo) StoreChallengeSetupSecret(ctx context.Context, challengeID id.ID, ciphertext, nonce []byte) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		UPDATE auth_mfa_challenges
		SET setup_secret_ciphertext = $2,
		    setup_secret_nonce = $3
		WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
	`
	tag, err := q.Exec(ctx, query, challengeID, ciphertext, nonce)
	if err != nil {
		return fmt.Errorf("store challenge setup secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewMFADenied("MFA challenge is invalid or expired")
	}
	return nil
}

func (r *MFARepo) ConsumeChallenge(ctx context.Context, challengeID id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		UPDATE auth_mfa_challenges
		SET consumed_at = now()
		WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
	`
	tag, err := q.Exec(ctx, query, challengeID)
	if err != nil {
		return fmt.Errorf("consume mfa challenge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewMFADenied("MFA challenge is invalid or expired")
	}
	return nil
}

func (r *MFARepo) CreateStepUpConfirmation(ctx context.Context, sessionID, userID id.ID, tenantID, actionCode string, expiresAt time.Time) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		INSERT INTO auth_step_up_confirmations (id, session_id, user_id, tenant_id, action_code, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	if _, err := q.Exec(ctx, query, id.New(), sessionID, userID, tenantID, actionCode, expiresAt); err != nil {
		return fmt.Errorf("create step-up confirmation: %w", err)
	}
	return nil
}

func (r *MFARepo) HasFreshStepUp(ctx context.Context, sessionID, userID id.ID, tenantID, actionCode string, now time.Time) (bool, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM auth_step_up_confirmations
			WHERE session_id = $1
			  AND user_id = $2
			  AND tenant_id = $3
			  AND action_code = $4
			  AND expires_at > $5
		)
	`
	var ok bool
	if err := q.QueryRow(ctx, query, sessionID, userID, tenantID, actionCode, now).Scan(&ok); err != nil {
		return false, fmt.Errorf("check step-up confirmation: %w", err)
	}
	return ok, nil
}

func (r *MFARepo) DeleteExpiredStepUps(ctx context.Context) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `DELETE FROM auth_step_up_confirmations WHERE expires_at <= now()`); err != nil {
		return fmt.Errorf("delete expired step-ups: %w", err)
	}
	return nil
}

var _ auth.MFARepository = (*MFARepo)(nil)
