//go:build integration

package crypto_integration_repo

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"metapus/internal/configuration/crypto/infrastructure/crypto/cryptoprofiles"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	integrationhttp "metapus/internal/infrastructure/integrations"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
)

func TestProjectRPCPoolProbes(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	var sepolia, nile id.ID
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE code='ETHEREUM-SEPOLIA'`).Scan(&sepolia))
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES ('ZAN-NILE','ZAN Nile','tron','nile','TRX',6) RETURNING id`).Scan(&nile))
	pools := map[integrations.Provider]id.ID{}
	const key = "test-project-key"
	for _, tt := range []struct {
		provider                     integrations.Provider
		slot, base, role, wireSuffix string
		networkID                    id.ID
	}{
		{cryptointegrations.ValidationCloud, "sepolia", "https://sepolia.ethereum.validationcloud.io/v1", "rpc", "/" + key, sepolia},
		{cryptointegrations.ZAN, "default", "https://api.zan.top/node/v1/eth/sepolia", "rpc", "/" + key, sepolia},
		{cryptointegrations.ZAN, "default", "https://api.zan.top/node/v1/tron/nile", "fullnode", "/" + key + "/wallet/getnowblock", nile},
		{cryptointegrations.ZAN, "default", "https://api.zan.top/node/v1/tron/nile", "solidity", "/" + key + "/walletsolidity/getnowblock", nile},
	} {
		t.Run(string(tt.provider)+"/"+tt.role, func(t *testing.T) {
			code := strings.ToUpper(string(tt.provider))
			poolID, exists := pools[tt.provider]
			if !exists {
				a := integrations.NewAccount(code, code)
				a.Provider = tt.provider
				a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: tt.slot, Value: key}}}
				ar := NewIntegrationAccountRepo()
				require.NoError(t, ar.Prepare(ctx, a, appDefinition(a.Provider)))
				require.NoError(t, ar.Create(ctx, a))
				p := integrations.NewPool(code, code)
				p.Provider, p.AccountIDs = a.Provider, []id.ID{a.ID}
				require.NoError(t, NewIntegrationPoolRepo().Create(ctx, p))
				poolID, pools[tt.provider] = p.ID, p.ID
			}
			e := rpcconfig.NewRPCEndpoint(code+"-"+tt.role, code+" "+tt.role)
			e.NetworkID, e.Role, e.ProviderID, e.TrustGroup = tt.networkID, tt.role, string(tt.provider), string(tt.provider)
			e.URL, e.PoolID = tt.base, &poolID
			require.NoError(t, NewCryptoRPCEndpointRepo().Create(ctx, e))
			require.Equal(t, tt.slot, e.Slot)
			target, err := NewConnectionTargets().ConnectionTarget(ctx, poolID)
			require.NoError(t, err)
			// One ZAN pool serves both networks. Exercise each concrete endpoint through the runtime resolver.
			target.EndpointID, target.URL = e.ID, e.URL
			target.Attributes = RPCTargetAttributes("evm", "11155111", tt.role)
			reply := `{"jsonrpc":"2.0","id":1,"result":"0xaa36a7"}`
			if tt.networkID == nile {
				target.Attributes = RPCTargetAttributes("tron", "nile", tt.role)
				reply = `{"blockID":"` + strings.Repeat("a", 64) + `","block_header":{"raw_data":{"number":100}}}`
			}
			calls := 0
			prober := integrationhttp.ConnectionProber{Profiles: cryptoprofiles.Registry(), EndpointProbe: RPCProbe, EndpointTransport: RPCTransport, Base: mockRPCTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, tt.base+tt.wireSuffix, req.URL.String())
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(reply))}, nil
			})}
			ok, err := prober.Probe(ctx, target)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, 1, calls)
		})
	}
	// Exhaustion applies across both networks. A recovery probe can discover ZAN's
	// 30-day renewal before the configured calendar-month reset.
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	runtime := integration_repo.NewRuntime(cryptointegrations.Catalog())
	runtime.Now = func() time.Time { return now }
	selectZAN := func() (integrations.Selection, error) {
		return runtime.Select(ctx, pools[cryptointegrations.ZAN], "default", cryptointegrations.ZAN)
	}
	selected, err := selectZAN()
	require.NoError(t, err)
	require.NoError(t, runtime.Report(ctx, selected, integrations.Exhausted, time.Time{}))
	_, err = selectZAN()
	var unavailable *integrations.PoolUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, integrations.Exhausted, unavailable.Reason)
	require.True(t, now.Add(time.Hour).Equal(unavailable.RetryAt))
	now = unavailable.RetryAt
	selected, err = selectZAN()
	require.NoError(t, err)
	require.True(t, selected.Recovery)
	require.NoError(t, runtime.Report(ctx, selected, "", time.Time{}))

	selected, err = selectZAN()
	require.NoError(t, err)
	require.NoError(t, runtime.Command(ctx, selected.PoolID, "exhaust", selected.SelectionVersion))
	// An in-flight gateway response cannot shorten the operator's manual hold.
	require.NoError(t, runtime.Report(ctx, selected, integrations.Exhausted, time.Time{}))
	_, err = selectZAN()
	require.ErrorAs(t, err, &unavailable)
	require.True(t, selected.PeriodEnd.Equal(unavailable.RetryAt))
}
