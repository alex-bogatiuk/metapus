package sdktest

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
)

// The fixtures below set up a database by hand, for a test that needs a
// database of its own rather than NewTenant's. At run time the platform does
// all of this when it binds a request or a worker to a tenant; configuration
// code never does it itself.

type (
	// TxManager runs work in transactions of one database pool.
	TxManager = postgres.TxManager
	// TxOptions are the isolation and access mode of a transaction.
	TxOptions = postgres.TxOptions
)

// Migrate migrates the database at dsn as the tenant command migrates a
// tenant database (migration.WithConfiguration names the configuration) and
// returns goose's output.
func Migrate(dsn string, opts ...migration.Option) (string, error) {
	return migration.RunAll(dsn, opts...)
}

// OpenPool opens a pool to the database at dsn with the platform's codecs.
func OpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return postgres.OpenPool(ctx, dsn)
}

// NewTxManager returns the transaction manager over pool that the platform
// gives a tenant's requests.
func NewTxManager(pool *pgxpool.Pool) *TxManager { return postgres.NewTxManagerFromRawPool(pool) }

// WithPool makes ctx carry pool as its tenant's database pool.
func WithPool(ctx context.Context, pool *pgxpool.Pool) context.Context {
	return tenantdb.WithPool(ctx, pool)
}
