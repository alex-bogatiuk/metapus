//go:build integration

package crypto_integration_repo

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

func accountWithKeys(code string, provider integrations.Provider, keys ...integrations.NamedKey) *integrations.Account {
	a := integrations.NewAccount(code, "Account "+code)
	a.Provider = provider
	a.PendingCredentials = &integrations.Credentials{Keys: keys}
	return a
}

func requireCredentialConflict(t *testing.T, err error, owner *integrations.Account, secret string) {
	t.Helper()
	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, apperror.CodeConflict, appErr.Code)
	require.Equal(t, http.StatusConflict, appErr.HTTPStatus)
	require.Equal(t, "credentials", appErr.Details["field"])
	require.Equal(t, owner.Name, appErr.Details["accountName"])
	require.Equal(t, owner.Code, appErr.Details["accountCode"])
	require.Contains(t, appErr.Message, owner.Name)
	require.NotContains(t, fmt.Sprint(appErr), secret)
	require.NotContains(t, fmt.Sprint(appErr.Details), secret)
}

func TestIntegrationAccountUniqueKeysAcrossSlotsAndUpdates(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	repo := catalog_repo.NewIntegrationAccountRepo()
	service := integrations.NewAccountService(repo, nil)
	const shared = "private-shared-secondary-key"
	owner := accountWithKeys("OWNER", integrations.Alchemy,
		integrations.NamedKey{Name: "default", Value: "private-primary-key"},
		integrations.NamedKey{Name: "secondary", Value: shared})
	owner.IsActive = false // Disabling an account must not free its credentials.
	require.NoError(t, service.Create(ctx, owner))

	duplicate := accountWithKeys("DUPLICATE", integrations.Alchemy,
		integrations.NamedKey{Name: "new", Value: "private-new-key"},
		integrations.NamedKey{Name: "renamed", Value: shared})
	duplicate.ExternalAccountRef = "different-external-account"
	requireCredentialConflict(t, service.Create(ctx, duplicate), owner, shared)
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM cat_integration_accounts WHERE id=$1`, duplicate.ID).Scan(&count))
	require.Zero(t, count)

	// Re-encrypting this account's own keys and editing metadata are allowed.
	loaded, err := repo.GetByID(ctx, owner.ID)
	require.NoError(t, err)
	loaded.Name = "Renamed owner"
	loaded.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "renamed", Value: shared}}}
	require.NoError(t, service.Update(ctx, loaded))
	require.NotEqual(t, owner.CredentialsEnc, loaded.CredentialsEnc)
	owner = loaded
	loaded, err = repo.GetByID(ctx, owner.ID)
	require.NoError(t, err)
	require.NoError(t, service.Update(ctx, loaded))
	owner = loaded

	other := accountWithKeys("OTHER", integrations.Alchemy, integrations.NamedKey{Name: "default", Value: "private-other-key"})
	require.NoError(t, service.Create(ctx, other))
	before, err := repo.GetByID(ctx, other.ID)
	require.NoError(t, err)
	other.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "default", Value: shared}}}
	requireCredentialConflict(t, service.Update(ctx, other), owner, shared)
	after, err := repo.GetByID(ctx, other.ID)
	require.NoError(t, err)
	require.Equal(t, before.CredentialsEnc, after.CredentialsEnc)
	require.Equal(t, before.CredentialsVersion, after.CredentialsVersion)
	require.Equal(t, before.Version, after.Version)

	// Providers own independent key namespaces; changing provider rechecks them.
	crossProvider := accountWithKeys("CROSS-PROVIDER", integrations.Chainstack, integrations.NamedKey{Name: "default", Value: shared})
	require.NoError(t, service.Create(ctx, crossProvider))
	crossProvider.Provider = integrations.Alchemy
	requireCredentialConflict(t, service.Update(ctx, crossProvider), owner, shared)
}

func TestIntegrationAccountUniqueKeysOnRestoreAndRemoval(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, _ := fixtureDB(t)
	repo := catalog_repo.NewIntegrationAccountRepo()
	service := integrations.NewAccountService(repo, nil)
	const shared = "private-restored-key"
	original := accountWithKeys("ORIGINAL", integrations.Alchemy, integrations.NamedKey{Name: "default", Value: shared})
	require.NoError(t, service.Create(ctx, original))
	require.NoError(t, service.SetDeletionMark(ctx, original.ID, true))
	replacement := accountWithKeys("REPLACEMENT", integrations.Alchemy, integrations.NamedKey{Name: "another-slot", Value: shared})
	require.NoError(t, service.Create(ctx, replacement))
	requireCredentialConflict(t, service.SetDeletionMark(ctx, original.ID, false), replacement, shared)
	stored, err := repo.GetByID(ctx, original.ID)
	require.NoError(t, err)
	require.True(t, stored.DeletionMark)

	// A normal update cannot bypass the restore check either.
	stored.DeletionMark = false
	requireCredentialConflict(t, service.Update(ctx, stored), replacement, shared)

	// Explicit removal of the replacement key frees it for the original account.
	replacement.PendingCredentials = &integrations.Credentials{}
	require.NoError(t, service.Update(ctx, replacement))
	require.NoError(t, service.SetDeletionMark(ctx, original.ID, false))
	stored, err = repo.GetByID(ctx, original.ID)
	require.NoError(t, err)
	require.False(t, stored.DeletionMark)
}

func TestIntegrationAccountConcurrentDuplicateKeys(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	repo := catalog_repo.NewIntegrationAccountRepo()
	const shared = "private-concurrent-key"
	accounts := []*integrations.Account{
		accountWithKeys("FIRST", integrations.Alchemy, integrations.NamedKey{Name: "default", Value: shared}),
		accountWithKeys("SECOND", integrations.Alchemy, integrations.NamedKey{Name: "different-slot", Value: shared}),
	}
	for _, account := range accounts {
		require.NoError(t, repo.Prepare(ctx, account))
	}
	require.NotEqual(t, accounts[0].CredentialsEnc, accounts[1].CredentialsEnc)
	start := make(chan struct{})
	type result struct {
		account *integrations.Account
		err     error
	}
	results := make(chan result, len(accounts))
	for _, account := range accounts {
		go func() {
			select {
			case <-ctx.Done():
				results <- result{account, ctx.Err()}
			case <-start:
				// Even direct repository writes acquire a transaction and lock.
				results <- result{account, repo.Create(ctx, account)}
			}
		}()
	}
	close(start)
	var winner *integrations.Account
	var failed error
	for range accounts {
		r := <-results
		if r.err == nil {
			require.Nil(t, winner, "only one concurrent write may succeed")
			winner = r.account
		} else {
			failed = r.err
		}
	}
	require.NotNil(t, winner)
	requireCredentialConflict(t, failed, winner, shared)
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM cat_integration_accounts`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestIntegrationAccountUniqueKeysAreTenantLocal(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	for range 2 {
		ctx, _ := fixtureDB(t)
		a := accountWithKeys("SAME", integrations.Alchemy, integrations.NamedKey{Name: "default", Value: "same-key-in-isolated-tenants"})
		require.NoError(t, integrations.NewAccountService(catalog_repo.NewIntegrationAccountRepo(), nil).Create(ctx, a))
	}
}

func TestIntegrationImportRejectsDuplicateKeys(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	const shared = "private-import-key"
	owner := accountWithKeys("EXISTING", integrations.Alchemy, integrations.NamedKey{Name: "default", Value: shared})
	require.NoError(t, integrations.NewAccountService(catalog_repo.NewIntegrationAccountRepo(), nil).Create(ctx, owner))
	var code string
	require.NoError(t, db.QueryRow(ctx, `SELECT code FROM cat_blockchain_networks WHERE chain_family='evm' LIMIT 1`).Scan(&code))
	cfg := &chainconfig.File{Networks: map[string]chainconfig.Network{
		code: {Roles: map[chainconfig.Role][]chainconfig.Endpoint{chainconfig.RoleRPC: {
			{ProviderID: "alchemy", URL: "https://eth-sepolia.g.alchemy.com/v2/" + shared},
		}}},
	}}
	bindings := map[string]ImportBinding{code + "/rpc/alchemy": {ExternalAccountRef: "new-external-account", Name: "Imported account", PoolCode: "IMPORT-POOL"}}
	err := tenant.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		_, err := ImportConfiguration(ctx, cfg, bindings, false)
		return err
	})
	requireCredentialConflict(t, err, owner, shared)
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM cat_integration_pools`).Scan(&count))
	require.Zero(t, count)
}
