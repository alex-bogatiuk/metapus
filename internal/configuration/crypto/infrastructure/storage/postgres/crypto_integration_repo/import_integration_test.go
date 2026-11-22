//go:build integration

package crypto_integration_repo

import (
	"context"
	"errors"
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/integrations"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/postgres/integration_repo"
)

func TestImportIsAtomicAndRepeatable(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	tm := postgres.MustGetTxManager(ctx)
	sourceID, currencyID, baseID, quoteID := id.New(), id.New(), id.New(), id.New()
	_, err := db.Exec(ctx, `INSERT INTO cat_rate_sources(id,code,name,source_type,api_key) VALUES($1,'LEGACY-CG','Legacy CoinGecko','coingecko','legacy-cg-key')`, sourceID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO cat_currencies(id,code,name,iso_code,symbol) VALUES($1,'TEST-A','A','TSA','A'),($2,'TEST-B','B','TSB','B')`, currencyID, baseID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO reg_rate_source_mappings(currency_id,base_currency_id,rate_source_id,external_id) VALUES($1,$2,$3,'test-coin')`, currencyID, baseID, sourceID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO reg_exchange_rates(id,currency_id,base_currency_id,rate_source_id,rate,provider_updated_at,received_at,version,mapping_version,feed_attempt_id) VALUES($1,$2,$3,$4,1.25,NOW(),NOW(),1,1,$5)`, quoteID, currencyID, baseID, sourceID, id.New())
	require.NoError(t, err)

	var networkID id.ID
	var code string
	require.NoError(t, db.QueryRow(ctx, `SELECT id,code FROM cat_blockchain_networks WHERE chain_family='evm' LIMIT 1`).Scan(&networkID, &code))
	cfg := &chainconfig.File{Networks: map[string]chainconfig.Network{code: {Roles: map[chainconfig.Role][]chainconfig.Endpoint{chainconfig.RoleRPC: {{ProviderID: "alchemy", URL: "https://eth-sepolia.g.alchemy.com/v2/import-test-key"}, {ProviderID: "publicnode", URL: "https://ethereum-sepolia-rpc.publicnode.com", Priority: 20}}}}}}
	bindings := map[string]ImportBinding{code + "/rpc/alchemy": {ExternalAccountRef: "alchemy-account-a", Name: "Alchemy A", PoolCode: "ALCHEMY"}, "rateSource:LEGACY-CG": {ExternalAccountRef: "cg-a", Name: "CoinGecko A", PoolCode: "CG-POOL"}}
	rollback := errors.New("dry-run")
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
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
	var accounts int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM cat_integration_accounts`).Scan(&accounts))
	require.Zero(t, accounts, "a rolled back import leaves no accounts")
	require.NoError(t, tm.RunInTransaction(ctx, func(ctx context.Context) error {
		_, e := ImportConfiguration(ctx, cfg, bindings, true)
		return e
	}))
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
	selected, err := integration_repo.NewRuntime(cryptointegrations.Catalog()).Select(ctx, *poolID, "default", integrations.CoinGeckoDemo)
	require.NoError(t, err)
	require.Equal(t, "legacy-cg-key", selected.Key)
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM reg_rate_source_mappings WHERE currency_id=$1 AND base_currency_id=$2 AND rate_source_id=$3 AND version=1`, currencyID, baseID, sourceID).Scan(&unchanged))
	require.Equal(t, 1, unchanged)
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM reg_exchange_rates WHERE id=$1 AND rate_source_id=$2 AND rate=1.25 AND version=1`, quoteID, sourceID).Scan(&unchanged))
	require.Equal(t, 1, unchanged)

}
