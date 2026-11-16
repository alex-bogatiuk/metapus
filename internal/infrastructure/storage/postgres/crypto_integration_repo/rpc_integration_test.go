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
	"metapus/internal/domain/crypto/rpcconfig"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
)

type mockRPCTransport func(*http.Request) (*http.Response, error)

func (f mockRPCTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRPCBindingPicksUpTenantChangesWithoutRestart(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr, er := NewIntegrationAccountRepo(), NewIntegrationPoolRepo(), NewCryptoRPCEndpointRepo()
	var accounts []*model.Account
	for _, code := range []string{"A", "B"} {
		a := model.NewAccount(code, code)
		a.PendingCredentials = &model.Credentials{Keys: []model.NamedKey{{Name: "default", Value: "key-" + code}}}
		require.NoError(t, ar.Prepare(ctx, a))
		require.NoError(t, ar.Create(ctx, a))
		accounts = append(accounts, a)
	}
	pool := model.NewPool("ALCHEMY", "Alchemy")
	pool.AccountIDs = []id.ID{accounts[0].ID, accounts[1].ID}
	require.NoError(t, pr.Create(ctx, pool))
	var networkID id.ID
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE chain_family='evm' LIMIT 1`).Scan(&networkID))
	endpoint := rpcconfig.NewRPCEndpoint("ALCHEMY-RPC", "Alchemy RPC")
	endpoint.NetworkID = networkID
	endpoint.URL = "https://eth-sepolia.g.alchemy.com/v2/"
	endpoint.TrustGroup = "alchemy"
	endpoint.ProviderID = "alchemy"
	endpoint.PoolID = &pool.ID
	require.NoError(t, er.Create(ctx, endpoint))
	now := time.Now()
	clock := func() time.Time { return now }
	var observed []string
	base := mockRPCTransport(func(r *http.Request) (*http.Response, error) {
		observed = append(observed, r.URL.Path)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"result":"0xaa36a7"}`))}, nil
	})
	worker := rpcTransportAt(endpoint.ID, endpoint.URL, clock)(base)
	monitor := rpcTransportAt(endpoint.ID, endpoint.URL, clock)(base)
	read := func(tr http.RoundTripper) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, strings.NewReader(`{"method":"eth_chainId"}`))
		require.NoError(t, err)
		resp, err := tr.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, "/v2/", req.URL.Path)
	}
	read(worker)
	read(monitor)
	require.Equal(t, []string{"/v2/key-A", "/v2/key-A"}, observed)
	accounts[0].IsActive = false
	require.NoError(t, ar.Update(ctx, accounts[0]))
	now = now.Add(11 * time.Second)
	read(worker)
	read(monitor)
	require.Equal(t, "/v2/key-B", observed[len(observed)-1])
	accounts[1].PendingCredentials = &model.Credentials{Keys: []model.NamedKey{{Name: "default", Value: "replacement-B"}}}
	require.NoError(t, ar.Prepare(ctx, accounts[1]))
	require.NoError(t, ar.Update(ctx, accounts[1]))
	now = now.Add(11 * time.Second)
	read(worker)
	read(monitor)
	require.Equal(t, "/v2/replacement-B", observed[len(observed)-1])
	state, err := integration_repo.NewRuntime().Status(ctx, pool.ID)
	require.NoError(t, err)
	require.Len(t, state.Accounts, 2)
	require.Equal(t, accounts[1].ID.String(), *state.AccountID)
	binding, err := NewConnectionTargets().ConnectionEndpoint(ctx, pool.ID)
	require.NoError(t, err)
	require.Equal(t, endpoint.ID, binding.ID)
	// No fallback to the startup URL after an explicit endpoint disable.
	endpoint.IsActive = false
	require.NoError(t, er.Update(ctx, endpoint))
	now = now.Add(11 * time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, nil)
	_, err = worker.RoundTrip(req)
	require.Error(t, err)
}
