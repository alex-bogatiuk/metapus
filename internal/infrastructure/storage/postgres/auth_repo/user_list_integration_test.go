//go:build integration

package auth_repo

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/storage/postgres"
)

type userListQueryCounter struct{ count atomic.Int32 }

func (c *userListQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.count.Add(1)
	return ctx
}
func (*userListQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestListUsersUsesFourQueriesAndPropagatesMFAReadErrors(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn, "explicit disposable PostgreSQL database is required")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	queries := &userListQueryCounter{}
	cfg.ConnConfig.Tracer = queries
	cfg.MaxConns = 1 // Keep these connection-local TEMP fixtures isolated.
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TEMP TABLE auth_users (
			id uuid PRIMARY KEY, email text, password_hash text DEFAULT '',
			first_name text DEFAULT '', last_name text DEFAULT '',
			is_active boolean DEFAULT true, is_admin boolean DEFAULT false,
			email_verified boolean DEFAULT false, email_verified_at timestamptz,
			last_login_at timestamptz, auth_version bigint DEFAULT 1,
			deletion_mark boolean DEFAULT false, version int DEFAULT 1, attributes jsonb DEFAULT '{}');
		CREATE TEMP TABLE auth_roles (id uuid PRIMARY KEY, code text, name text, description text, is_system boolean);
		CREATE TEMP TABLE auth_user_roles (user_id uuid, role_id uuid);
		CREATE TEMP TABLE auth_user_mfa_totp (user_id uuid PRIMARY KEY, enabled boolean, required_after timestamptz);
	`)
	require.NoError(t, err)
	ids := make([]id.ID, 100)
	for i := range ids {
		ids[i] = id.New()
		_, err = pool.Exec(ctx, `INSERT INTO auth_users(id, email) VALUES ($1::uuid, $1::uuid::text || '@batch.test')`, ids[i])
		require.NoError(t, err)
	}
	roleID := id.New()
	grace := time.Now().UTC().Truncate(time.Second)
	_, err = pool.Exec(ctx, `INSERT INTO auth_roles VALUES ($1,'security_admin','Security admin','',true);
		INSERT INTO auth_user_roles VALUES ($2,$1);
		INSERT INTO auth_user_mfa_totp VALUES ($2,true,$3);`, pgx.QueryExecModeSimpleProtocol, roleID, ids[0], grace)
	require.NoError(t, err)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	u, m := NewUserRepo(), NewMFARepo()
	service := auth.NewService(u, nil, nil, nil, m, nil, nil, nil, txm, nil, nil, nil, auth.ServiceConfig{AdminMFAMode: auth.MFAModeRequired})
	for _, limit := range []int{1, 100} {
		queries.count.Store(0)
		users, total, err := service.ListUsers(ctx, auth.UserFilter{Limit: limit})
		require.NoError(t, err)
		require.Len(t, users, limit)
		require.Equal(t, 100, total)
		require.EqualValues(t, 4, queries.count.Load(), "COUNT + users + roles + non-secret MFA statuses")
		require.Equal(t, ids[0], users[0].ID)
		require.True(t, users[0].MFAEnabled)
		require.True(t, users[0].MFARequired)
		require.True(t, grace.Equal(*users[0].MFAGraceEndsAt))
		if limit > 1 {
			require.False(t, users[1].MFAEnabled)
			require.Empty(t, users[1].Roles)
		}
	}
	roles, err := u.LoadRoles(ctx, ids[0])
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, roleID, roles[0].ID)
	require.Equal(t, "security_admin", roles[0].Code)

	// Empty batches need neither a tenant context nor a database connection.
	queries.count.Store(0)
	rolesByUser, err := u.LoadRolesForUsers(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, rolesByUser)
	statuses, err := m.LoadMFAStatuses(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, statuses)
	require.Zero(t, queries.count.Load())

	// A broken status source must never turn into a successful "MFA disabled" list.
	_, err = pool.Exec(ctx, `ALTER TABLE pg_temp.auth_user_mfa_totp RENAME COLUMN enabled TO unavailable`)
	require.NoError(t, err)
	users, _, err := service.ListUsers(ctx, auth.UserFilter{Limit: 1})
	require.Error(t, err)
	require.Nil(t, users)
}
