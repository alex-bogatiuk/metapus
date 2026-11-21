//go:build integration

package integration_repo

import (
	"context"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/migration"
)

func fixtureDB(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_INTEGRATIONS_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_INTEGRATIONS_DSN must identify an isolated test cluster")
	}
	root, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(root.Close)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	name := "integrations_" + id.Compact(id.New())
	_, err = root.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := root.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		if err != nil {
			t.Errorf("clean up integration fixture database: %v", err)
		}
	})
	u.Path = "/" + name
	output, err := migration.RunAll(u.String())
	require.NoError(t, err, output)
	p, err := pgxpool.New(context.Background(), u.String())
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return tenant.WithTxManager(context.Background(), postgres.NewTxManagerFromRawPool(p)), p
}
func TestIntegrationAccountsPersistQuotaAndConcurrentSelection(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	accounts := catalog_repo.NewIntegrationAccountRepo()
	pools := catalog_repo.NewIntegrationPoolRepo()
	var ids []id.ID
	for _, name := range []string{"a", "b", "c"} {
		a := integrations.NewAccount(name, name)
		a.Provider = integrations.CoinGeckoDemo
		a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "default", Value: "private-" + name}}}
		require.NoError(t, accounts.Prepare(ctx, a, platformDefinition(t, a.Provider)))
		require.NoError(t, accounts.Create(ctx, a))
		ids = append(ids, a.ID)
	}
	p := integrations.NewPool("pool", "Pool")
	p.Provider = integrations.CoinGeckoDemo
	p.AccountIDs = ids
	require.NoError(t, pools.ValidateMembers(ctx, p))
	require.NoError(t, pools.Create(ctx, p))
	loaded, err := pools.GetByID(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, ids, loaded.AccountIDs)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	r := NewRuntime(integrations.PlatformCatalog())
	r.Now = func() time.Time { return now }
	a, err := r.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, ids[0], a.AccountID)
	require.Equal(t, "private-a", a.Key)
	var ciphertext []byte
	require.NoError(t, db.QueryRow(ctx, `SELECT credentials_enc FROM cat_integration_accounts WHERE id=$1`, ids[0]).Scan(&ciphertext))
	require.NotContains(t, string(ciphertext), "private-a")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- r.Report(ctx, a, integrations.Exhausted, time.Time{}) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	b, err := r.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, ids[1], b.AccountID)
	// A delayed failure from A must not advance the now healthy B cursor.
	require.NoError(t, r.Report(ctx, a, integrations.Exhausted, time.Time{}))
	again, err := NewRuntimeWithClock(now).Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, ids[1], again.AccountID)
	require.NoError(t, r.Report(ctx, b, integrations.Exhausted, time.Time{}))
	c, err := r.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, ids[2], c.AccountID)
	require.NoError(t, r.Report(ctx, c, integrations.Exhausted, time.Time{}))
	_, err = r.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.Error(t, err)
	now = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	reset, err := r.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, ids[0], reset.AccountID)
	require.True(t, reset.Recovery)
	require.NoError(t, r.Report(ctx, reset, "", time.Time{}))
	reset, err = r.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, ids[0], reset.AccountID)
	require.False(t, reset.Recovery)
	// Old period reports have no effect on the new quota period.
	require.NoError(t, r.Report(ctx, a, integrations.Exhausted, time.Time{}))
	reset, err = r.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, ids[0], reset.AccountID)
	// Credential replacement unblocks an invalid version, without changing account identity.
	require.NoError(t, r.Report(ctx, reset, integrations.Unauthorized, time.Time{}))
	first, err := accounts.GetByID(ctx, ids[0])
	require.NoError(t, err)
	first.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "default", Value: "replacement"}}}
	require.NoError(t, accounts.Prepare(ctx, first, platformDefinition(t, first.Provider)))
	require.NoError(t, accounts.Update(ctx, first))
	single := integrations.NewPool("single", "Single restored account")
	single.Provider = integrations.CoinGeckoDemo
	single.AccountIDs = []id.ID{ids[0]}
	require.NoError(t, pools.Create(ctx, single))
	restored, err := r.Select(ctx, single.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, ids[0], restored.AccountID)
	require.Equal(t, "replacement", restored.Key)
}

// platformDefinition returns a platform provider's definition; these tests
// use only platform providers.
func platformDefinition(t *testing.T, provider integrations.Provider) integrations.ProviderDefinition {
	t.Helper()
	definition, err := integrations.PlatformCatalog().Require(provider)
	require.NoError(t, err)
	return definition
}

func NewRuntimeWithClock(now time.Time) *Runtime {
	r := NewRuntime(integrations.PlatformCatalog())
	r.Now = func() time.Time { return now }
	return r
}

func TestTenantIsolationAndCatalogReferenceGuards(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, permissionsDB := fixtureDB(t)
	other, _ := fixtureDB(t)
	var granted int
	require.NoError(t, permissionsDB.QueryRow(ctx, `SELECT count(*) FROM auth_role_permissions rp JOIN auth_roles r ON r.id=rp.role_id JOIN auth_permissions p ON p.id=rp.permission_id WHERE r.code='admin' AND p.code IN('catalog:integration_account:read','catalog:integration_account:create','catalog:integration_account:update','catalog:integration_account:delete','catalog:integration_pool:read','catalog:integration_pool:create','catalog:integration_pool:update','catalog:integration_pool:delete')`).Scan(&granted))
	require.Equal(t, 8, granted)
	require.NoError(t, permissionsDB.QueryRow(ctx, `SELECT count(*) FROM auth_role_permissions rp JOIN auth_roles r ON r.id=rp.role_id JOIN auth_permissions p ON p.id=rp.permission_id WHERE r.code<>'admin' AND p.resource IN('integration_account','integration_pool')`).Scan(&granted))
	require.Zero(t, granted)
	ar, pr := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo()
	a := integrations.NewAccount("A", "Account A")
	a.Provider = integrations.CoinGeckoDemo
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "default", Value: "private-a"}}}
	require.NoError(t, ar.Prepare(ctx, a, platformDefinition(t, a.Provider)))
	require.NoError(t, ar.Create(ctx, a))
	p := integrations.NewPool("P", "Pool")
	p.Provider = integrations.CoinGeckoDemo
	p.AccountIDs = []id.ID{a.ID}
	require.NoError(t, pr.Create(ctx, p))
	_, err := ar.GetByID(other, a.ID)
	require.Error(t, err)
	_, err = NewRuntime(integrations.PlatformCatalog()).Select(other, p.ID, "default", integrations.CoinGeckoDemo)
	require.Error(t, err)
	require.Error(t, ar.SetDeletionMark(ctx, a.ID, true))
	a.Provider = integrations.Telegram
	require.Error(t, ar.Prepare(ctx, a, platformDefinition(t, a.Provider)))
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	r := NewRuntimeWithClock(now)
	s, err := r.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.NoError(t, r.Report(ctx, s, integrations.Exhausted, time.Time{}))
	next := NewRuntimeWithClock(time.Date(2026, 11, 1, 0, 0, 1, 0, time.UTC))
	probe, err := next.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.True(t, probe.Recovery)
	_, err = next.Select(ctx, p.ID, "default", integrations.CoinGeckoDemo)
	require.Error(t, err, "only one process may probe a recovered account")
}
