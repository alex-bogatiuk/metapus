// Package auth_repo provides PostgreSQL implementations for auth repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context.
package auth_repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/storage/postgres"
)

// TokenRepo implements auth.TokenRepository.
// In Database-per-Tenant, TxManager is obtained from context.
type TokenRepo struct{}

// NewTokenRepo creates a new token repository.
func NewTokenRepo() *TokenRepo {
	return &TokenRepo{}
}

// getTxManager retrieves TxManager from context.
func (r *TokenRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// SaveRefreshToken saves a refresh token.
func (r *TokenRepo) SaveRefreshToken(ctx context.Context, token *auth.RefreshToken) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::inet)
	`

	_, err := q.Exec(ctx, query,
		token.ID, token.UserID, token.TokenHash, token.ExpiresAt,
		token.CreatedAt, token.UserAgent, token.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}

	return nil
}

// GetRefreshToken retrieves refresh token by hash.
func (r *TokenRepo) GetRefreshToken(ctx context.Context, tokenHash string) (*auth.RefreshToken, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, user_id, token_hash, expires_at, created_at, revoked_at, revoked_reason
		FROM refresh_tokens WHERE token_hash = $1
	`

	var token auth.RefreshToken
	err := q.QueryRow(ctx, query, tokenHash).Scan(
		&token.ID, &token.UserID, &token.TokenHash, &token.ExpiresAt,
		&token.CreatedAt, &token.RevokedAt, &token.RevokedReason,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("token", "")
	}
	if err != nil {
		return nil, fmt.Errorf("query token: %w", err)
	}

	return &token, nil
}

// RevokeRefreshToken revokes a refresh token.
func (r *TokenRepo) RevokeRefreshToken(ctx context.Context, tokenID id.ID, reason string) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `UPDATE refresh_tokens SET revoked_at = now(), revoked_reason = $2 WHERE id = $1`
	_, err := q.Exec(ctx, query, tokenID, reason)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	return nil
}

// RevokeAllUserTokens revokes all tokens for a user.
func (r *TokenRepo) RevokeAllUserTokens(ctx context.Context, userID id.ID, reason string) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `UPDATE refresh_tokens SET revoked_at = now(), revoked_reason = $2 WHERE user_id = $1 AND revoked_at IS NULL`
	_, err := q.Exec(ctx, query, userID, reason)
	if err != nil {
		return fmt.Errorf("revoke all tokens: %w", err)
	}

	return nil
}

// CleanupExpiredTokens removes expired tokens.
func (r *TokenRepo) CleanupExpiredTokens(ctx context.Context) (int, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `DELETE FROM refresh_tokens WHERE expires_at < now() OR revoked_at < now() - INTERVAL '7 days'`
	result, err := q.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("cleanup tokens: %w", err)
	}

	return int(result.RowsAffected()), nil
}

// Ensure interface compliance
var _ auth.TokenRepository = (*TokenRepo)(nil)
