package merchant_provisioning

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/vaultclient"
)

func TestControlPlaneVaultDerivesAndBindsBothFamilies(t *testing.T) {
	for _, family := range []crypto.ChainFamily{crypto.ChainFamilyTRON, crypto.ChainFamilyEVM} {
		t.Run(string(family), func(t *testing.T) {
			namespace, merchant := id.New(), id.New()
			identifier, pluginNetwork := "nile", "tron_nile"
			if family == crypto.ChainFamilyEVM {
				identifier, pluginNetwork = "11155111", "11155111"
			}
			calls := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.True(t, strings.HasPrefix(r.URL.Path, "/v1/"+string(family)+"/tenants/"+namespace.String()+"/"))
				calls = append(calls, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "config/readiness"):
					_, _ = io.WriteString(w, `{"data":{"merchant_custody_supported":true,"merchant_custody_schema":"merchant_custody.v1","merchant_custody_revision":1,"merchant_custody_networks":["`+pluginNetwork+`"]}}`)
				case strings.HasSuffix(r.URL.Path, "wallets/derive"):
					_, _ = io.WriteString(w, `{"data":{"address":"synthetic-address","vault_key_id":"synthetic-key","derivation_path":"m/0/1"}}`)
				case strings.HasSuffix(r.URL.Path, "wallets/custody"):
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					var request map[string]string
					require.NoError(t, json.Unmarshal(body, &request))
					require.Equal(t, merchant.String(), request["merchant_id"])
					require.Equal(t, "liquidity_hot", request["role"])
					require.Equal(t, pluginNetwork, request["network"])
					_, _ = io.WriteString(w, `{"data":{"schema_version":"merchant_custody.v1","network":"`+pluginNetwork+`","address":"synthetic-address","merchant_id":"`+merchant.String()+`","role":"liquidity_hot"}}`)
				default:
					t.Errorf("unexpected Vault endpoint %s", r.URL.Path)
				}
			}))
			defer server.Close()
			client, err := vaultclient.New(vaultclient.Config{Addr: server.URL, MaxRetries: 1})
			require.NoError(t, err)
			vault, err := New(client, namespace)
			require.NoError(t, err)
			derived, binding, err := vault.DeriveAndEnroll(t.Context(), namespace, crypto.NetworkRef{Family: family, NetworkIdentifier: identifier}, merchant, "liquidity_hot", "m/0/1")
			require.NoError(t, err)
			require.Equal(t, "synthetic-key", derived.VaultKeyID)
			require.Equal(t, merchant.String(), binding.MerchantID)
			require.Len(t, calls, 3)
			_, _, err = vault.DeriveAndEnroll(t.Context(), id.New(), crypto.NetworkRef{Family: family, NetworkIdentifier: identifier}, merchant, "liquidity_hot", "m/0/1")
			require.ErrorContains(t, err, "namespace mismatch")
			require.Len(t, calls, 3)
		})
	}
}
