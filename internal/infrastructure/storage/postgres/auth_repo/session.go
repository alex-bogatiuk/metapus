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

// SessionRepo implements auth.AuthStateRepository.
// In Database-per-Tenant, TxManager is obtained from context.
type SessionRepo struct{}

// NewSessionRepo creates a new auth session repository.
func NewSessionRepo() *SessionRepo {
	return &SessionRepo{}
}

func (r *SessionRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// CreateSession creates a server-side auth session.
func (r *SessionRepo) CreateSession(ctx context.Context, session *auth.AuthSession) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	const query = `
		INSERT INTO auth_sessions (
			id, user_id, user_auth_version, policy_version,
			created_at, expires_at, user_agent, ip_address
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::inet)
	`

	_, err := q.Exec(ctx, query,
		session.ID,
		session.UserID,
		session.UserAuthVersion,
		session.PolicyVersion,
		session.CreatedAt,
		session.ExpiresAt,
		session.UserAgent,
		session.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

// GetSessionState retrieves current server-side auth state for a session.
func (r *SessionRepo) GetSessionState(ctx context.Context, userID, sessionID id.ID) (*auth.AuthSessionState, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	const query = `
		SELECT
			s.id,
			s.user_id,
			u.auth_version,
			p.version,
			u.is_active,
			s.expires_at,
			s.revoked_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id AND u.deletion_mark = FALSE
		CROSS JOIN auth_policy_state p
		WHERE s.id = $1 AND s.user_id = $2
	`

	var state auth.AuthSessionState
	err := q.QueryRow(ctx, query, sessionID, userID).Scan(
		&state.SessionID,
		&state.UserID,
		&state.UserAuthVersion,
		&state.PolicyVersion,
		&state.UserActive,
		&state.ExpiresAt,
		&state.RevokedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("auth_session", sessionID.String())
	}
	if err != nil {
		return nil, fmt.Errorf("get auth session state: %w", err)
	}
	return &state, nil
}

// RevokeSession revokes a single session.
func (r *SessionRepo) RevokeSession(ctx context.Context, sessionID id.ID, reason string) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	const query = `
		UPDATE auth_sessions
		SET revoked_at = now(), revoked_reason = $2
		WHERE id = $1 AND revoked_at IS NULL
	`
	_, err := q.Exec(ctx, query, sessionID, reason)
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	return nil
}

// RevokeAllUserSessions revokes all sessions for a user.
func (r *SessionRepo) RevokeAllUserSessions(ctx context.Context, userID id.ID, reason string) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	const query = `
		UPDATE auth_sessions
		SET revoked_at = now(), revoked_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL
	`
	_, err := q.Exec(ctx, query, userID, reason)
	if err != nil {
		return fmt.Errorf("revoke user auth sessions: %w", err)
	}
	return nil
}

// ExtendSession updates session expiry and last-seen metadata on refresh.
func (r *SessionRepo) ExtendSession(ctx context.Context, sessionID id.ID, expiresAt time.Time, info auth.SessionInfo) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	const query = `
		UPDATE auth_sessions
		SET expires_at = $2,
		    last_seen_at = now(),
		    user_agent = COALESCE(NULLIF($3, ''), user_agent),
		    ip_address = COALESCE(NULLIF($4, '')::inet, ip_address)
		WHERE id = $1 AND revoked_at IS NULL
	`
	_, err := q.Exec(ctx, query, sessionID, expiresAt, info.UserAgent, info.IPAddress)
	if err != nil {
		return fmt.Errorf("extend auth session: %w", err)
	}
	return nil
}

// BumpUserAuthVersion invalidates existing access tokens for one user.
func (r *SessionRepo) BumpUserAuthVersion(ctx context.Context, userID id.ID) (int64, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	const query = `
		UPDATE users
		SET auth_version = auth_version + 1
		WHERE id = $1 AND deletion_mark = FALSE
		RETURNING auth_version
	`
	var version int64
	err := q.QueryRow(ctx, query, userID).Scan(&version)
	if err == pgx.ErrNoRows {
		return 0, apperror.NewNotFound("user", userID.String())
	}
	if err != nil {
		return 0, fmt.Errorf("bump user auth version: %w", err)
	}
	return version, nil
}

// GetCurrentPolicyVersion returns the tenant-local RBAC policy epoch.
func (r *SessionRepo) GetCurrentPolicyVersion(ctx context.Context) (int64, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	if err := r.ensurePolicyState(ctx); err != nil {
		return 0, err
	}

	const query = `SELECT version FROM auth_policy_state WHERE id = TRUE`
	var version int64
	if err := q.QueryRow(ctx, query).Scan(&version); err != nil {
		return 0, fmt.Errorf("get auth policy version: %w", err)
	}
	return version, nil
}

// BumpPolicyVersion invalidates tokens that carry the old RBAC policy epoch.
func (r *SessionRepo) BumpPolicyVersion(ctx context.Context) (int64, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	const query = `
		INSERT INTO auth_policy_state (id, version, updated_at)
		VALUES (TRUE, 1, now())
		ON CONFLICT (id) DO UPDATE
		SET version = auth_policy_state.version + 1,
		    updated_at = now()
		RETURNING version
	`
	var version int64
	if err := q.QueryRow(ctx, query).Scan(&version); err != nil {
		return 0, fmt.Errorf("bump auth policy version: %w", err)
	}
	return version, nil
}

func (r *SessionRepo) ensurePolicyState(ctx context.Context) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)
	const query = `
		INSERT INTO auth_policy_state (id, version, updated_at)
		VALUES (TRUE, 1, now())
		ON CONFLICT (id) DO NOTHING
	`
	if _, err := q.Exec(ctx, query); err != nil {
		return fmt.Errorf("ensure auth policy state: %w", err)
	}
	return nil
}

var _ auth.AuthStateRepository = (*SessionRepo)(nil)
