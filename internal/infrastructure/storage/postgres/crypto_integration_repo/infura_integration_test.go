//go:build integration

package crypto_integration_repo

import (
	"io"
	"metapus/internal/domain/crypto/cryptointegrations"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
)

func TestInfuraEncryptedKeyBindingAndDailyStatus(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr, er := NewIntegrationAccountRepo(), NewIntegrationPoolRepo(), NewCryptoRPCEndpointRepo()
	secret := "0123456789abcdef0123456789abcdef"
	a := integrations.NewAccount("INFURA", "Infura Sepolia")
	a.Provider = cryptointegrations.Infura
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: secret}}}
	require.NoError(t, a.Validate(ctx))
	require.NoError(t, ar.Prepare(ctx, a, appDefinition(a.Provider)))
	require.NoError(t, ar.Create(ctx, a))
	var ciphertext []byte
	require.NoError(t, db.QueryRow(ctx, `SELECT credentials_enc FROM cat_integration_accounts WHERE id=$1`, a.ID).Scan(&ciphertext))
	require.NotContains(t, string(ciphertext), secret)
	pool := integrations.NewPool("INFURA", "Infura")
	pool.Provider, pool.AccountIDs = cryptointegrations.Infura, []id.ID{a.ID}
	require.NoError(t, pr.Create(ctx, pool))
	var networkID id.ID
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE code='ETHEREUM-SEPOLIA'`).Scan(&networkID))
	e := rpcconfig.NewRPCEndpoint("INFURA-RPC", "Infura Sepolia")
	e.NetworkID, e.ProviderID, e.TrustGroup = networkID, "infura-sepolia", "infura"
	e.URL, e.PoolID, e.Slot = "https://sepolia.infura.io/v3/", &pool.ID, "sepolia"
	require.NoError(t, er.Create(ctx, e))
	tr := RPCTransport(e.ID, e.URL)(mockRPCTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/v3/"+secret, r.URL.Path)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"result":"0xaa36a7"}`))}, nil
	}))
	defer tr.(interface{ CloseIdleConnections() }).CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL, nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, "https://sepolia.infura.io/v3/", req.URL.String())
	state, err := integration_repo.NewRuntime(cryptointegrations.Catalog()).Status(ctx, pool.ID)
	require.NoError(t, err)
	require.Len(t, state.Accounts, 1)
	require.Equal(t, time.Now().UTC().Truncate(24*time.Hour).Add(24*time.Hour), *state.Accounts[0].NextResetAt)
	e.TrustGroup = "infura-another-key"
	require.Error(t, er.ValidateBinding(ctx, e))
	e.TrustGroup, e.URL = "infura", e.URL+secret
	require.Error(t, er.ValidateBinding(ctx, e))
	e.URL, e.PoolID = "https://sepolia.infura.io/v3/", nil
	require.Error(t, er.ValidateBinding(ctx, e))
	one := chainconfig.Endpoint{URL: "https://sepolia.infura.io/v3/", ProviderID: "a", TrustGroup: "pretend-a"}
	two := chainconfig.Endpoint{URL: "https://sepolia.infura.io/v3/", ProviderID: "b", TrustGroup: "pretend-b"}
	require.Equal(t, "infura", one.TrustIdentity())
	require.Equal(t, one.TrustIdentity(), two.TrustIdentity())
}
