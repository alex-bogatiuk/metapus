//go:build integration

package sdktest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/configuration"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
)

// Tenant is a fresh tenant database migrated with a configuration.
type Tenant struct {
	tenant *testsupport.SecurityTenant
	// Pool is the tenant database.
	Pool *pgxpool.Pool
}

// NewTenant creates a fresh tenant database in the integration test cluster
// (METAPUS_TEST_DATABASE_DSN) and migrates it as the tenant command migrates
// a tenant of cfg: the platform's, cfg's and the extensions' migrations,
// then cfg's permissions.
func NewTenant(t *testing.T, cfg configuration.Configuration) *Tenant {
	t.Helper()
	tenant := testsupport.NewSecurityTenants(t, configuration.MigrationOptions(cfg)...)[0]
	return &Tenant{tenant: tenant, Pool: tenant.Pool}
}

// AdminContext is the context of an administrator of the tenant: the
// platform's repositories and posting run in it.
func (t *Tenant) AdminContext() context.Context {
	return t.tenant.Context(appctx.UserContext{UserID: id.New().String(), IsAdmin: true})
}

// RunInTransaction runs fn in a transaction of the tenant database, as the
// platform runs a posting.
func (t *Tenant) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return t.tenant.Tx.RunInTransaction(ctx, fn)
}

// NewTenantWithMigrations is NewTenant for a test inside a configuration's
// own packages, which cannot import the configuration itself: the tenant
// database is migrated with opts (migration.WithConfiguration) instead.
func NewTenantWithMigrations(t *testing.T, opts ...migration.Option) *Tenant {
	t.Helper()
	tenant := testsupport.NewSecurityTenants(t, opts...)[0]
	return &Tenant{tenant: tenant, Pool: tenant.Pool}
}

// Context is the context of user in the tenant: the platform's
// repositories run in it under user's permissions.
func (t *Tenant) Context(user appctx.UserContext) context.Context {
	return t.tenant.Context(user)
}

// NewExchangeRateDatabase creates a fresh empty database in the exchange
// rate test cluster (METAPUS_TEST_EXCHANGE_RATES_DSN) and returns its DSN;
// the test migrates it itself (migration.RunAll).
func NewExchangeRateDatabase(t *testing.T) string {
	t.Helper()
	return testsupport.ExchangeRateDSN(t)
}
