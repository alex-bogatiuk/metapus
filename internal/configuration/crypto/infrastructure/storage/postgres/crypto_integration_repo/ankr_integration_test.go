//go:build integration

package crypto_integration_repo

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto/cryptoprofiles"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/transport"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/integration_repo"
)

func TestAnkrFreemiumSepoliaPoolProbe(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	const base = "https://rpc.ankr.com/eth_sepolia"
	key := strings.Repeat("ab", 32)
	account := integrations.NewAccount("ANKR", "Ankr Freemium")
	account.Provider = cryptointegrations.Ankr
	account.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: key}}}
	definition := appDefinition(account.Provider)
	require.Equal(t, integrations.QuotaMonthly, definition.QuotaPolicy)
	require.NoError(t, account.ValidateFor(definition))
	accounts := NewIntegrationAccountRepo()
	require.NoError(t, accounts.Prepare(ctx, account, definition))
	require.NoError(t, accounts.Create(ctx, account))

	pool := integrations.NewPool("ANKR", "Ankr Sepolia")
	pool.Provider, pool.AccountIDs = account.Provider, []id.ID{account.ID}
	require.NoError(t, NewIntegrationPoolRepo().Create(ctx, pool))
	endpoint := rpcconfig.NewRPCEndpoint("ANKR-SEP", "Ankr Sepolia")
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE code='ETHEREUM-SEPOLIA'`).Scan(&endpoint.NetworkID))
	endpoint.URL, endpoint.ProviderID, endpoint.TrustGroup = base, "ankr-sepolia", "ankr"
	endpoints := NewCryptoRPCEndpointRepo()
	require.Error(t, endpoints.ValidateBinding(ctx, endpoint), "Ankr requires a pool")
	endpoint.PoolID = &pool.ID
	require.NoError(t, endpoints.Create(ctx, endpoint))
	target, err := NewConnectionTargets().ConnectionTarget(ctx, pool.ID)
	require.NoError(t, err)
	require.Equal(t, base, target.URL)
	require.Equal(t, "sepolia", endpoint.Slot)

	reply := `{"jsonrpc":"2.0","id":1,"result":"0xaa36a7"}`
	headers := http.Header{}
	prober := transport.ConnectionProber{
		Profiles: cryptoprofiles.Registry(), EndpointProbe: RPCProbe, EndpointTransport: RPCTransport,
		Base: mockRPCTransport(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, base+"/"+key, r.URL.String())
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`, string(body))
			return &http.Response{StatusCode: http.StatusOK, Header: headers.Clone(), Body: io.NopCloser(strings.NewReader(reply))}, nil
		}),
	}
	ok, err := prober.Probe(ctx, target)
	require.NoError(t, err)
	require.True(t, ok)
	reply = `{"jsonrpc":"2.0","id":1,"result":"0x1"}`
	ok, err = prober.Probe(ctx, target)
	require.NoError(t, err)
	require.False(t, ok)

	retry := time.Now().UTC().Add(time.Minute).Truncate(time.Second)
	headers.Set("Retry-After", retry.Format(http.TimeFormat))
	reply = `{"jsonrpc":"2.0","id":1,"error":{"code":-32090,"message":"call rate limit exhausted"}}`
	ok, err = prober.Probe(ctx, target)
	require.NoError(t, err)
	require.False(t, ok)
	state, err := integration_repo.NewRuntime(cryptointegrations.Catalog()).Status(ctx, pool.ID)
	require.NoError(t, err)
	require.Len(t, state.Accounts, 1)
	require.NotNil(t, state.Accounts[0].ThrottledUntil)
	require.True(t, retry.Equal(*state.Accounts[0].ThrottledUntil))
	require.Nil(t, state.Accounts[0].ExhaustedUntil)
}
