//go:build integration

package integration_repo

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/tron"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

func TestAlchemyTRONBindingsReloadKeysAndPreserveRoleMethods(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr, er := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo(), catalog_repo.NewCryptoRPCEndpointRepo()
	a := integrations.NewAccount("ALCH-NILE", "Alchemy Nile")
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "nile", Value: "nile-key"}}}
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Create(ctx, a))
	pool := integrations.NewPool("ALCH-NILE", "Alchemy Nile")
	pool.AccountIDs = []id.ID{a.ID}
	require.NoError(t, pr.Create(ctx, pool))
	var nile, sepolia id.ID
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES('ALCH-NILE','Alchemy test Nile','tron','nile','TRX',6) RETURNING id`).Scan(&nile))
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE chain_family='evm' LIMIT 1`).Scan(&sepolia))
	current := "nile-key"
	for _, role := range []string{"fullnode", "solidity"} {
		e := integrations.NewRPCEndpoint("ALCH-"+role, "Alchemy "+role)
		e.NetworkID, e.Role, e.ProviderID, e.TrustGroup = nile, role, "alchemy-nile", "alchemy"
		e.URL, e.PoolID, e.Slot = "https://tron-testnet.g.alchemy.com/v2/", &pool.ID, "nile"
		require.NoError(t, er.Create(ctx, e))
		copy := *e
		copy.NetworkID, copy.Role = sepolia, "rpc"
		require.Error(t, er.ValidateBinding(ctx, &copy))
		copy = *e
		copy.URL = "https://tron-mainnet.g.alchemy.com/v2/"
		require.Error(t, er.ValidateBinding(ctx, &copy))
		clock := time.Now()
		var observed string
		tr := rpcTransportAt(e.ID, e.URL, func() time.Time { return clock })(mockRPCTransport(func(r *http.Request) (*http.Response, error) {
			observed = r.URL.Path
			if strings.HasSuffix(observed, "/gettransactioninfobyid") {
				require.Equal(t, http.MethodPost, r.Method)
				require.Empty(t, r.URL.RawQuery)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"value":"transaction-hash"}`, string(body))
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}))
		t.Cleanup(func() { tr.(interface{ CloseIdleConnections() }).CloseIdleConnections() })
		client := tron.NewClient(tron.ClientConfig{BaseURL: e.URL, WrapTransport: func(http.RoundTripper) http.RoundTripper { return tr }})
		method := "/wallet/getnowblock"
		if role == "solidity" {
			method = "/walletsolidity/getnowblock"
		}
		read := func() {
			if role == "fullnode" {
				_, err := client.GetCurrentBlockInfo(ctx)
				require.NoError(t, err)
				_, err = client.GetTransactionInfo(ctx, "transaction-hash")
				require.NoError(t, err)
			} else {
				_, err := client.GetSolidifiedCurrentBlockInfo(ctx)
				require.NoError(t, err)
				_, err = client.GetSolidifiedTransactionInfo(ctx, "transaction-hash")
				require.NoError(t, err)
			}
		}
		method = strings.TrimSuffix(method, "/getnowblock") + "/gettransactioninfobyid"
		read()
		require.Equal(t, "/v2/"+current+method, observed)
		current = "replacement-" + role
		a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "nile", Value: current}}}
		require.NoError(t, ar.Prepare(ctx, a))
		require.NoError(t, ar.Update(ctx, a))
		clock = clock.Add(11 * time.Second)
		read()
		require.Equal(t, "/v2/replacement-"+role+method, observed)
	}
}
