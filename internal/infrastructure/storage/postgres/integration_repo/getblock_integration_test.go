//go:build integration

package integration_repo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

func TestGetBlockEncryptedRoleSlotsAndTrustBoundary(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr, er := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo(), catalog_repo.NewCryptoRPCEndpointRepo()
	a := integrations.NewAccount("GETBLOCK-NILE", "GetBlock Nile")
	a.Provider, a.ExternalAccountRef = integrations.GetBlock, "one-getblock-subscription"
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "fullnode", Value: "fullnode-secret"}, {Name: "solidity", Value: "solidity-secret"}}}
	require.NoError(t, a.Validate(ctx))
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Create(ctx, a))
	var ciphertext []byte
	require.NoError(t, db.QueryRow(ctx, `SELECT credentials_enc FROM cat_integration_accounts WHERE id=$1`, a.ID).Scan(&ciphertext))
	require.NotContains(t, string(ciphertext), "fullnode-secret")
	require.NotContains(t, string(ciphertext), "solidity-secret")
	pool := integrations.NewPool("GETBLOCK-NILE", "GetBlock Nile")
	pool.Provider, pool.AccountIDs = integrations.GetBlock, []id.ID{a.ID}
	require.NoError(t, pr.ValidateMembers(ctx, pool))
	require.NoError(t, pr.Create(ctx, pool))
	var networkID id.ID
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES('GETBLOCK-TEST-NILE','GetBlock test Nile','tron','nile','TRX',6) RETURNING id`).Scan(&networkID))
	for _, role := range []string{"fullnode", "solidity"} {
		e := integrations.NewRPCEndpoint("GETBLOCK-"+role, role)
		e.NetworkID, e.Role, e.ProviderID, e.TrustGroup = networkID, role, "getblock-nile", "getblock"
		e.URL, e.PoolID, e.Slot = "https://shared.eu-central-1.getblock.io", &pool.ID, role
		require.NoError(t, er.Create(ctx, e))
		prefix := "/wallet/"
		if role == "solidity" {
			prefix = "/walletsolidity/"
		}
		transport := RPCTransport(e.ID, e.URL)(mockRPCTransport(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "/"+role+"-secret"+prefix+"getnowblock", req.URL.Path)
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL+prefix+"getnowblock", nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
		e.TrustGroup = "getblock-second-token"
		require.Error(t, er.ValidateBinding(ctx, e), "role tokens must never become independent trust groups")
		e.TrustGroup, e.Slot = "getblock", "default"
		require.Error(t, er.ValidateBinding(ctx, e), "role must select its own token slot")
		e.Slot, e.URL = role, e.URL+"/secret"
		require.Error(t, er.ValidateBinding(ctx, e), "credential URL must never be stored in RPC configuration")
	}
}

func TestGetBlockImportIsAtomicRepeatableAndPreservesNetworkSettings(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	tm := postgres.MustGetTxManager(ctx)
	var networkID id.ID
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals,rpc_config_source) VALUES('GETBLOCK-IMPORT','GetBlock import','tron','nile','TRX',6,'database') RETURNING id`).Scan(&networkID))
	_, err := db.Exec(ctx, `INSERT INTO sys_crypto_rpc_network_settings(network_id,tron_settings) VALUES($1,'{"ingestionMode":"hybrid","readBudgetMs":14000}')`, networkID)
	require.NoError(t, err)
	cfg := &chainconfig.File{Networks: map[string]chainconfig.Network{"GETBLOCK-IMPORT": {Roles: map[chainconfig.Role][]chainconfig.Endpoint{}}}}
	bindings := map[string]ImportBinding{}
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		cfg.Networks["GETBLOCK-IMPORT"].Roles[role] = []chainconfig.Endpoint{{ProviderID: "getblock-nile", TrustGroup: "getblock", URL: "https://shared.eu-central-1.getblock.io", APIKey: string(role) + "-test-token", Priority: 40}}
		bindings["GETBLOCK-IMPORT/"+string(role)+"/getblock-nile"] = ImportBinding{ExternalAccountRef: "one-subscription", Name: "GetBlock Nile", PoolCode: "GETBLOCK-IMPORT", Slot: string(role)}
	}
	dryRun := errors.New("dry run")
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
		r, err := ImportConfiguration(ctx, cfg, bindings, false)
		require.NoError(t, err)
		require.Equal(t, ImportReport{Accounts: 1, Pools: 1, Endpoints: 2}, r)
		return dryRun
	})
	require.ErrorIs(t, err, dryRun)
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM cat_integration_accounts WHERE provider='getblock'`).Scan(&count))
	require.Zero(t, count)
	for i := range 2 {
		require.NoError(t, tm.RunInTransaction(ctx, func(ctx context.Context) error {
			r, err := ImportConfiguration(ctx, cfg, bindings, false)
			if i == 1 {
				require.Equal(t, ImportReport{}, r)
			}
			return err
		}))
	}
	var unchanged bool
	require.NoError(t, db.QueryRow(ctx, `SELECT tron_settings='{"ingestionMode":"hybrid","readBudgetMs":14000}'::jsonb FROM sys_crypto_rpc_network_settings WHERE network_id=$1`, networkID).Scan(&unchanged))
	require.True(t, unchanged)
	var safe bool
	require.NoError(t, db.QueryRow(ctx, `SELECT bool_and(url='https://shared.eu-central-1.getblock.io' AND trust_group='getblock' AND credential_slot=role) FROM sys_crypto_rpc_endpoints WHERE network_id=$1`, networkID).Scan(&safe))
	require.True(t, safe)
}
