//go:build integration

package integration_repo

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

func TestInfuraUpgradeEncryptedKeyBindingAndDailyStatus(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	for _, table := range []string{"cat_integration_accounts", "cat_integration_pools"} {
		_, err := db.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s_provider_check, ADD CONSTRAINT %s_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','telegram','smtp','resource_api'))", table, table, table))
		require.NoError(t, err)
	}
	needed, err := UpgradeSchema(ctx, false)
	require.NoError(t, err)
	require.True(t, needed)
	var definition string
	require.NoError(t, db.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='cat_integration_accounts_provider_check'`).Scan(&definition))
	require.NotContains(t, definition, "infura")
	needed, err = UpgradeSchema(ctx, true)
	require.NoError(t, err)
	require.True(t, needed)
	needed, err = UpgradeSchema(ctx, true)
	require.NoError(t, err)
	require.False(t, needed)
	ar, pr, er := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo(), catalog_repo.NewCryptoRPCEndpointRepo()
	secret := "0123456789abcdef0123456789abcdef"
	a := integrations.NewAccount("INFURA", "Infura Sepolia")
	a.Provider = integrations.Infura
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: secret}}}
	require.NoError(t, a.Validate(ctx))
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Create(ctx, a))
	var ciphertext []byte
	require.NoError(t, db.QueryRow(ctx, `SELECT credentials_enc FROM cat_integration_accounts WHERE id=$1`, a.ID).Scan(&ciphertext))
	require.NotContains(t, string(ciphertext), secret)
	pool := integrations.NewPool("INFURA", "Infura")
	pool.Provider, pool.AccountIDs = integrations.Infura, []id.ID{a.ID}
	require.NoError(t, pr.Create(ctx, pool))
	var networkID id.ID
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE code='ETHEREUM-SEPOLIA'`).Scan(&networkID))
	e := integrations.NewRPCEndpoint("INFURA-RPC", "Infura Sepolia")
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
	state, err := NewRuntime().Status(ctx, pool.ID)
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
