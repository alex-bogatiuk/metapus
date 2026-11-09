//go:build integration

package integration_repo

import (
	"context"
	"database/sql"
	"errors"
	"github.com/pressly/goose/v3"
	"io/fs"
	dbassets "metapus/db"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestUpgradeAndImportAreAtomicAndRepeatable(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	// This fixture simulates the pre-AML schema. Later migrations intentionally
	// reference integration accounts, so first roll back their empty schemas.
	migrationDB, err := sql.Open("pgx", db.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrationDB.Close() })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, migrationDB, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 60)
	require.NoError(t, err)

	tm := postgres.MustGetTxManager(ctx)
	sourceID, currencyID, baseID, quoteID := id.New(), id.New(), id.New(), id.New()
	_, err = db.Exec(ctx, `INSERT INTO cat_rate_sources(id,code,name,source_type,api_key) VALUES($1,'LEGACY-CG','Legacy CoinGecko','coingecko','legacy-cg-key')`, sourceID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO cat_currencies(id,code,name,iso_code,symbol) VALUES($1,'TEST-A','A','TSA','A'),($2,'TEST-B','B','TSB','B')`, currencyID, baseID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO reg_rate_source_mappings(currency_id,base_currency_id,rate_source_id,external_id) VALUES($1,$2,$3,'test-coin')`, currencyID, baseID, sourceID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO reg_exchange_rates(id,currency_id,base_currency_id,rate_source_id,rate,provider_updated_at,received_at,version,mapping_version,feed_attempt_id) VALUES($1,$2,$3,$4,1.25,NOW(),NOW(),1,1,$5)`, quoteID, currencyID, baseID, sourceID, id.New())
	require.NoError(t, err)

	// Simulate a filled pre-feature schema without resetting its existing data.
	_, err = db.Exec(ctx, `ALTER TABLE cat_rate_sources DROP COLUMN integration_pool_id;ALTER TABLE cat_blockchain_networks DROP COLUMN rpc_config_source;DROP TABLE sys_crypto_rpc_endpoints,sys_crypto_rpc_network_settings,sys_integration_pool_state,sys_integration_account_state,sys_integration_pool_accounts,cat_integration_pools,cat_integration_accounts;DELETE FROM auth_role_permissions WHERE permission_id IN(SELECT id FROM auth_permissions WHERE resource IN ('integration_account','integration_pool','crypto_rpc_endpoint'));DELETE FROM auth_permissions WHERE resource IN ('integration_account','integration_pool','crypto_rpc_endpoint');`)
	require.NoError(t, err)
	needed, err := UpgradeSchema(ctx, false)
	require.NoError(t, err)
	require.True(t, needed)
	var absent bool
	require.NoError(t, db.QueryRow(ctx, `SELECT to_regclass('cat_integration_accounts') IS NULL`).Scan(&absent))
	require.True(t, absent)
	var networkID id.ID
	var code string
	require.NoError(t, db.QueryRow(ctx, `SELECT id,code FROM cat_blockchain_networks WHERE chain_family='evm' LIMIT 1`).Scan(&networkID, &code))
	cfg := &chainconfig.File{Networks: map[string]chainconfig.Network{code: {Roles: map[chainconfig.Role][]chainconfig.Endpoint{chainconfig.RoleRPC: {{ProviderID: "alchemy", URL: "https://eth-sepolia.g.alchemy.com/v2/import-test-key"}, {ProviderID: "publicnode", URL: "https://ethereum-sepolia-rpc.publicnode.com", Priority: 20}}}}}}
	bindings := map[string]ImportBinding{code + "/rpc/alchemy": {ExternalAccountRef: "alchemy-account-a", Name: "Alchemy A", PoolCode: "ALCHEMY"}, "rateSource:LEGACY-CG": {ExternalAccountRef: "cg-a", Name: "CoinGecko A", PoolCode: "CG-POOL"}}
	rollback := errors.New("dry-run")
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
		_, e := UpgradeSchema(ctx, true)
		if e != nil {
			return e
		}
		r, e := ImportConfiguration(ctx, cfg, bindings, true)
		if e != nil {
			return e
		}
		require.Equal(t, 2, r.Accounts)
		require.Equal(t, 1, r.RateSources)
		require.Equal(t, 2, r.Endpoints)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	require.NoError(t, db.QueryRow(ctx, `SELECT to_regclass('cat_integration_accounts') IS NULL`).Scan(&absent))
	require.True(t, absent)
	require.NoError(t, tm.RunInTransaction(ctx, func(ctx context.Context) error {
		_, e := UpgradeSchema(ctx, true)
		if e != nil {
			return e
		}
		_, e = ImportConfiguration(ctx, cfg, bindings, true)
		return e
	}))
	needed, err = UpgradeSchema(ctx, true)
	require.NoError(t, err)
	require.False(t, needed)
	require.NoError(t, tm.RunInTransaction(ctx, func(ctx context.Context) error {
		r, e := ImportConfiguration(ctx, cfg, bindings, true)
		require.Equal(t, ImportReport{}, r)
		return e
	}))
	var storedURL, source string
	require.NoError(t, db.QueryRow(ctx, `SELECT url FROM sys_crypto_rpc_endpoints WHERE network_id=$1 AND provider_id='alchemy'`, networkID).Scan(&storedURL))
	require.Equal(t, "https://eth-sepolia.g.alchemy.com/v2/", storedURL)
	require.NoError(t, db.QueryRow(ctx, `SELECT rpc_config_source FROM cat_blockchain_networks WHERE id=$1`, networkID).Scan(&source))
	require.Equal(t, "database", source)
	loaded, err := LoadRPCNetwork(ctx, crypto.NetworkRef{ID: networkID}, chainconfig.Network{})
	require.NoError(t, err)
	require.Len(t, loaded.Endpoints(chainconfig.RoleRPC), 2)
	var poolID *id.ID
	var legacyKey *string
	var unchanged int
	require.NoError(t, db.QueryRow(ctx, `SELECT integration_pool_id,api_key FROM cat_rate_sources WHERE id=$1`, sourceID).Scan(&poolID, &legacyKey))
	require.NotNil(t, poolID)
	require.Nil(t, legacyKey)
	selected, err := NewRuntime().Select(ctx, *poolID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, "legacy-cg-key", selected.Key)
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM reg_rate_source_mappings WHERE currency_id=$1 AND base_currency_id=$2 AND rate_source_id=$3 AND version=1`, currencyID, baseID, sourceID).Scan(&unchanged))
	require.Equal(t, 1, unchanged)
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM reg_exchange_rates WHERE id=$1 AND rate_source_id=$2 AND rate=1.25 AND version=1`, quoteID, sourceID).Scan(&unchanged))
	require.Equal(t, 1, unchanged)

}
