//go:build integration

package catalog_repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
)

func TestIntegrationLookups(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := tenant.Context(appctx.UserContext{})
	account := func(code, provider string, active, marked bool) id.ID {
		uid := id.New()
		_, err := tenant.Pool.Exec(ctx, `INSERT INTO cat_integration_accounts(id,code,name,provider,is_active,deletion_mark) VALUES($1,$2,$2,$3,$4,$5)`, uid, code, provider, active, marked)
		require.NoError(t, err)
		return uid
	}
	pool := func(code, provider string, marked bool, members ...id.ID) id.ID {
		uid := id.New()
		_, err := tenant.Pool.Exec(ctx, `INSERT INTO cat_integration_pools(id,code,name,provider,deletion_mark) VALUES($1,$2,$2,$3,$4)`, uid, code, provider, marked)
		require.NoError(t, err)
		for position, member := range members {
			_, err := tenant.Pool.Exec(ctx, `INSERT INTO sys_integration_pool_accounts(pool_id,account_id,position) VALUES($1,$2,$3)`, uid, member, position)
			require.NoError(t, err)
		}
		return uid
	}
	usable := account("A-USABLE", "lookup_alpha", true, false)
	inactive := account("A-INACTIVE", "lookup_alpha", false, false)
	marked := account("A-MARKED", "lookup_alpha", true, true)
	other := account("A-OTHER", "lookup_beta", true, false)
	own := pool("P-OWN", "lookup_alpha", false, usable, inactive)
	shared := pool("P-SHARED", "lookup_alpha", false, inactive, other)
	apart := pool("P-APART", "lookup_beta", false, other)
	gone := pool("P-GONE", "lookup_alpha", true)

	provider, err := catalog_repo.PoolProvider(ctx, own)
	require.NoError(t, err)
	require.EqualValues(t, "lookup_alpha", provider)
	_, err = catalog_repo.PoolProvider(ctx, gone)
	require.True(t, apperror.IsNotFound(err), "%v", err)

	pools, err := catalog_repo.PoolsSharingAccounts(ctx, own)
	require.NoError(t, err)
	require.ElementsMatch(t, []id.ID{own, shared}, pools)
	pools, err = catalog_repo.AccountPoolIDs(ctx, other)
	require.NoError(t, err)
	require.ElementsMatch(t, []id.ID{shared, apart}, pools)
	pools, err = catalog_repo.PoolsSharingAccounts(ctx, gone)
	require.NoError(t, err)
	require.Empty(t, pools)

	provider, err = catalog_repo.AccountProvider(ctx, inactive)
	require.NoError(t, err)
	require.EqualValues(t, "lookup_alpha", provider)
	all, err := catalog_repo.ProviderAccountIDs(ctx, "lookup_alpha")
	require.NoError(t, err)
	require.ElementsMatch(t, []id.ID{usable, inactive, marked}, all)
	ready, err := catalog_repo.UsableAccountIDs(ctx, "lookup_alpha")
	require.NoError(t, err)
	require.Equal(t, []id.ID{usable}, ready)

	require.NoError(t, catalog_repo.RequireUsableAccount(ctx, usable, "lookup_alpha"))
	for _, tt := range []struct {
		name     string
		account  id.ID
		provider integrations.Provider
	}{
		{name: "inactive", account: inactive, provider: "lookup_alpha"},
		{name: "marked", account: marked, provider: "lookup_alpha"},
		{name: "another provider", account: usable, provider: "lookup_beta"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := catalog_repo.RequireUsableAccount(ctx, tt.account, tt.provider)
			require.True(t, apperror.IsNotFound(err), "%v", err)
		})
	}

	require.NoError(t, tenant.Tx.RunInTransaction(ctx, func(ctx context.Context) error {
		return catalog_repo.LockUsableAccount(ctx, usable, "lookup_alpha")
	}))
	err = tenant.Tx.RunInTransaction(ctx, func(ctx context.Context) error {
		return catalog_repo.LockUsableAccount(ctx, inactive, "lookup_alpha")
	})
	require.True(t, apperror.IsNotFound(err), "%v", err)

	keyless := id.New()
	require.NoError(t, catalog_repo.CreateKeylessAccount(ctx, keyless, "A-KEYLESS", "Без ключа", "lookup_beta"))
	require.NoError(t, catalog_repo.RequireUsableAccount(ctx, keyless, "lookup_beta"))
	credentials, err := catalog_repo.AccountCredentials(ctx, keyless)
	require.NoError(t, err)
	require.Empty(t, credentials.Keys)
}
