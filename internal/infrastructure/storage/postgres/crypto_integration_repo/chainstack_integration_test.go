//go:build integration

package crypto_integration_repo

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
)

func TestChainstackPoolExhaustionRecoveryAndRPCBindings(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr, er := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo(), catalog_repo.NewCryptoRPCEndpointRepo()
	pool := integrations.NewPool("CHAINSTACK", "Chainstack")
	pool.Provider = integrations.Chainstack
	for _, code := range []string{"A", "B"} {
		a := integrations.NewAccount(code, code)
		a.Provider = integrations.Chainstack
		a.ExternalAccountRef = "account-" + code
		a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "nile", Value: "key-" + code}}}
		require.NoError(t, ar.Prepare(ctx, a))
		require.NoError(t, ar.Create(ctx, a))
		pool.AccountIDs = append(pool.AccountIDs, a.ID)
	}
	require.NoError(t, pr.Create(ctx, pool))
	var networkID id.ID
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES('CHAINSTACK-TEST-NILE','Chainstack test Nile','tron','nile','TRX',6) RETURNING id`).Scan(&networkID))
	var fullnode *integrations.RPCEndpoint
	for _, role := range []string{"fullnode", "solidity"} {
		e := integrations.NewRPCEndpoint("CHAINSTACK-"+role, role)
		e.NetworkID = networkID
		e.Role = role
		e.ProviderID = "chainstack-nile"
		e.TrustGroup = "chainstack"
		e.URL = "https://tron-nile.core.chainstack.com"
		e.PoolID = &pool.ID
		e.Slot = "nile"
		require.NoError(t, er.Create(ctx, e))
		if role == "fullnode" {
			copy := *e
			fullnode = &copy
		}
		e.Role = "indexer"
		require.Error(t, er.ValidateBinding(ctx, e))
	}
	clock := time.Now()
	var observed string
	transport := rpcTransportAt(fullnode.ID, fullnode.URL, func() time.Time { return clock })(mockRPCTransport(func(req *http.Request) (*http.Response, error) {
		observed = req.URL.Path
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}))
	t.Cleanup(func() {
		if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	})
	read := func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullnode.URL+"/wallet/getnowblock", nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	read()
	require.Equal(t, "/key-A/wallet/getnowblock", observed)
	a, err := ar.GetByID(ctx, pool.AccountIDs[0])
	require.NoError(t, err)
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "nile", Value: "replacement-A"}}}
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Update(ctx, a))
	clock = clock.Add(11 * time.Second)
	read()
	require.Equal(t, "/replacement-A/wallet/getnowblock", observed)
	r := integration_repo.NewRuntime()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	r.Now = func() time.Time { return now }
	for _, uid := range pool.AccountIDs {
		s, err := r.Select(ctx, pool.ID, "nile", integrations.Chainstack)
		require.NoError(t, err)
		require.Equal(t, uid, s.AccountID)
		require.NoError(t, r.Report(ctx, s, integrations.Exhausted, time.Time{}))
	}
	_, err = r.Select(ctx, pool.ID, "nile", integrations.Chainstack)
	var unavailable *integrations.PoolUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, integrations.Exhausted, unavailable.Reason)
	require.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), unavailable.RetryAt.UTC())
	now = unavailable.RetryAt.Add(time.Second)
	s, err := r.Select(ctx, pool.ID, "nile", integrations.Chainstack)
	require.NoError(t, err)
	require.True(t, s.Recovery)
	require.NoError(t, r.Report(ctx, s, "", time.Time{}))
}

func TestChainstackSepoliaBindingIsNetworkScopedAndReloadsKeys(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr, er := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo(), catalog_repo.NewCryptoRPCEndpointRepo()
	a := integrations.NewAccount("CS-EVM", "Chainstack EVM")
	a.Provider = integrations.Chainstack
	a.ExternalAccountRef = "separate-evm-account"
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: "evm-key"}}}
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Create(ctx, a))
	pool := integrations.NewPool("CS-EVM", "Chainstack EVM")
	pool.Provider = integrations.Chainstack
	pool.AccountIDs = []id.ID{a.ID}
	require.NoError(t, pr.Create(ctx, pool))
	var sepolia, mainnet, tron id.ID
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE chain_family='evm' AND network_identifier='11155111' LIMIT 1`).Scan(&sepolia))
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES('CS-MAINNET','Mainnet','evm','1','ETH',18) RETURNING id`).Scan(&mainnet))
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES('CS-TRON','TRON','tron','nile','TRX',6) RETURNING id`).Scan(&tron))
	e := integrations.NewRPCEndpoint("CS-SEPOLIA", "Chainstack Sepolia")
	e.NetworkID = sepolia
	e.ProviderID = "chainstack-sepolia"
	e.TrustGroup = "chainstack"
	e.URL = "https://ethereum-sepolia.core.chainstack.com"
	e.PoolID = &pool.ID
	e.Slot = "sepolia"
	require.NoError(t, er.Create(ctx, e))
	for _, network := range []id.ID{mainnet, tron} {
		copy := *e
		copy.NetworkID = network
		require.Error(t, er.ValidateBinding(ctx, &copy))
	}
	copy := *e
	copy.PoolID = nil
	require.Error(t, er.ValidateBinding(ctx, &copy))
	copy = *e
	copy.TrustGroup = "chainstack-second-account"
	require.Error(t, er.ValidateBinding(ctx, &copy))
	copy = *e
	copy.URL = "https://tron-nile.core.chainstack.com"
	require.Error(t, er.ValidateBinding(ctx, &copy))
	clock := time.Now()
	var path string
	tr := rpcTransportAt(e.ID, e.URL, func() time.Time { return clock })(mockRPCTransport(func(r *http.Request) (*http.Response, error) {
		path = r.URL.Path
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"result":"0xaa36a7"}`))}, nil
	}))
	t.Cleanup(func() {
		if closer, ok := tr.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	})
	read := func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`))
		require.NoError(t, err)
		resp, err := tr.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	read()
	require.Equal(t, "/evm-key", path)
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: "replacement-evm"}}}
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Update(ctx, a))
	clock = clock.Add(11 * time.Second)
	read()
	require.Equal(t, "/replacement-evm", path)
	binding, err := NewConnectionTargets().ConnectionEndpoint(ctx, pool.ID)
	require.NoError(t, err)
	require.Equal(t, "rpc", binding.Role)
}
