//go:build integration

package auth_repo

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	"os"
	"testing"
)

func TestRefreshCleanupPreservesRecentRevocationsIncludingExpiredTokens(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)
	rollback := errors.New("rollback fixture")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		userID, sessionID := id.New(), id.New()
		_, err := q.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash) VALUES($1,$1::text||'@cleanup.test','fixture');
		INSERT INTO auth_sessions(id,user_id,user_auth_version,policy_version,expires_at) VALUES($2,$1,1,1,NOW()+interval '30 days');`, pgx.QueryExecModeSimpleProtocol, userID, sessionID)
		require.NoError(t, err)
		for _, c := range []struct{ name, expiry, revoke string }{
			{"active", "1 day", ""}, {"expired", "-1 day", ""}, {"recent-revoked", "1 day", "-1 day"},
			{"expired-recent-revoked", "-1 day", "-1 day"}, {"old-revoked", "1 day", "-8 days"},
		} {
			_, err = q.Exec(ctx, `INSERT INTO auth_refresh_tokens(id,user_id,session_id,token_hash,expires_at,revoked_at)
			VALUES($1,$2,$3,$4,NOW()+$5::interval,CASE WHEN $6='' THEN NULL ELSE NOW()+$6::interval END)`, id.New(), userID, sessionID, userID.String()+c.name, c.expiry, c.revoke)
			require.NoError(t, err)
		}
		_, err = NewTokenRepo().CleanupExpiredTokens(ctx)
		require.NoError(t, err)
		rows, err := q.Query(ctx, `SELECT REPLACE(token_hash,$1,'') FROM auth_refresh_tokens WHERE user_id=$2 ORDER BY token_hash`, userID.String(), userID)
		require.NoError(t, err)
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		require.NoError(t, err)
		require.Equal(t, []string{"active", "expired-recent-revoked", "recent-revoked"}, names)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
