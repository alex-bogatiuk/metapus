//go:build integration

package testsupport

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/migration"
)

// SecurityTenant owns a fresh database. IDs deliberately overlap across tenants
// in tests, so tenant isolation cannot accidentally pass by UUID uniqueness.
type SecurityTenant struct {
	ID   id.ID
	Pool *pgxpool.Pool
	Tx   *postgres.TxManager
}

func NewSecurityTenants(t *testing.T) [2]*SecurityTenant {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn, "explicit isolated PostgreSQL DSN is required")
	target, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, target.Scheme)
	ctx := context.Background()
	cluster, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer cluster.Close()
	require.NoError(t, cluster.Ping(ctx))
	var tenants [2]*SecurityTenant
	for i := range tenants {
		name := "metapus_security_" + id.Compact(id.New())
		_, err := cluster.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
		require.NoError(t, err)
		target.Path = "/" + name
		output, err := migration.RunAll(target.String())
		require.NoError(t, err, output)
		pool, err := pgxpool.New(ctx, target.String())
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		tenants[i] = &SecurityTenant{ID: id.New(), Pool: pool, Tx: postgres.NewTxManagerFromRawPool(pool)}
	}
	return tenants
}

func (s *SecurityTenant) Context(principal appctx.UserContext) context.Context {
	principal.TenantID = s.ID.String()
	return tenant.WithTxManager(appctx.WithUser(context.Background(), &principal), s.Tx)
}
