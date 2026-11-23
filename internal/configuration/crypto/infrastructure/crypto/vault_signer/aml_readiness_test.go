package vault_signer

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto/evm_vault_signer"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/stretchr/testify/require"
)

func TestAMLReadinessRejectsCustodyOnlyPluginsForBothFamilies(t *testing.T) {
	valid := crypto.AMLSigningContract{Schema: crypto.AMLSigningSchemaV1, IntentSchema: crypto.MoneyMovementIntentV2, SourceLineBasis: "CryptoAMLWithdrawalLine"}
	for _, family := range []crypto.ChainFamily{crypto.ChainFamilyTRON, crypto.ChainFamilyEVM} {
		for _, tc := range []struct {
			name     string
			contract crypto.AMLSigningContract
			ready    bool
			status   int
		}{
			{"custody_only", crypto.AMLSigningContract{}, false, 200},
			{"intent_v1", crypto.AMLSigningContract{Schema: valid.Schema, IntentSchema: crypto.MoneyMovementIntentV1, SourceLineBasis: valid.SourceLineBasis}, false, 200},
			{"classless_basis", crypto.AMLSigningContract{Schema: valid.Schema, IntentSchema: valid.IntentSchema, SourceLineBasis: "WithdrawalRequest"}, false, 200},
			{"upgraded", valid, true, 200}, {"unavailable", valid, false, 503},
		} {
			t.Run(string(family)+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					require.Equal(t, http.MethodGet, r.Method)
					require.Equal(t, "/v1/"+string(family)+"/tenants/"+testTenantID+"/config/readiness", r.URL.Path)
					w.WriteHeader(tc.status)
					_ = json.NewEncoder(w).Encode(map[string]any{"data": tc.contract})
				}))
				defer server.Close()
				client, err := vaultclient.New(vaultclient.Config{Addr: server.URL, Token: "synthetic"})
				require.NoError(t, err)
				var guard crypto.AMLSigner
				if family == crypto.ChainFamilyTRON {
					guard, err = New(Config{TenantID: testTenantID, Client: client})
				} else {
					guard, err = evm_vault_signer.New(evm_vault_signer.Config{TenantID: testTenantID, Client: client})
				}
				require.NoError(t, err)
				err = guard.CheckAMLReady(t.Context(), crypto.NetworkRef{Family: family, NetworkIdentifier: "test"})
				if tc.ready {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				if tc.status == http.StatusOK {
					require.EqualValues(t, 1, calls.Load())
				} else {
					require.GreaterOrEqual(t, calls.Load(), int32(1))
				}
			})
		}
	}
}
