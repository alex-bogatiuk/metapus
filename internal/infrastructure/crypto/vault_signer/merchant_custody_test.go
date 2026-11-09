package vault_signer

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/crypto/evm_vault_signer"
	"metapus/internal/infrastructure/vaultclient"
)

func TestMerchantCustodyReadinessRequiresIndependentEnforcement(t *testing.T) {
	for _, family := range []crypto.ChainFamily{crypto.ChainFamilyTRON, crypto.ChainFamilyEVM} {
		t.Run(string(family), func(t *testing.T) {
			network := crypto.NetworkRef{Family: family, NetworkIdentifier: "mainnet"}
			pluginNetwork := "tron_mainnet"
			if family == crypto.ChainFamilyEVM {
				network.NetworkIdentifier = "11155111"
				pluginNetwork = network.NetworkIdentifier
			}
			for _, tc := range []struct {
				name      string
				supported bool
				schema    string
				revision  int
				networks  []string
				ready     bool
			}{
				{name: "legacy plugin"},
				{name: "capability without policy", supported: true, schema: "merchant_custody.v1"},
				{name: "unknown schema", supported: true, schema: "merchant_custody.v2", revision: 1, networks: []string{pluginNetwork}},
				{name: "wrong network", supported: true, schema: "merchant_custody.v1", revision: 1, networks: []string{"other"}},
				{name: "enforced", supported: true, schema: "merchant_custody.v1", revision: 1, networks: []string{pluginNetwork}, ready: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, http.MethodGet, r.Method)
						require.Equal(t, "/v1/"+string(family)+"/tenants/"+testTenantID+"/config/readiness", r.URL.Path)
						_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"merchant_custody_supported": tc.supported, "merchant_custody_schema": tc.schema, "merchant_custody_revision": tc.revision, "merchant_custody_networks": tc.networks}})
					}))
					defer server.Close()
					var guard crypto.MerchantCustodySigner
					if family == crypto.ChainFamilyTRON {
						guard = newTestSigner(t, server.URL, "test-token")
					} else {
						client, err := vaultclient.New(vaultclient.Config{Addr: server.URL, Token: "test-token"})
						require.NoError(t, err)
						signer, err := evm_vault_signer.New(evm_vault_signer.Config{TenantID: testTenantID, Client: client})
						require.NoError(t, err)
						guard = signer
					}
					err := guard.CheckMerchantCustodyReady(t.Context(), network)
					if tc.ready {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
				})
			}
		})
	}
}
