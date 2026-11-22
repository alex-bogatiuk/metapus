package vault_signer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domaincrypto "metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/pkg/sdk/json"
)

const testTenantID = "00000000-0000-0000-0000-000000000001"

func TestVaultSigner_GenerateTRONAddress(t *testing.T) {
	tests := []struct {
		name          string
		network       string
		mockStatus    int
		mockResponse  any
		wantResult    domaincrypto.WalletDerivation
		wantErrPrefix string
	}{
		{
			name:       "success",
			network:    "tron_mainnet",
			mockStatus: http.StatusOK,
			mockResponse: map[string]any{
				"data": map[string]string{
					"address":         "TJBwE3H6y2H3c4x1j3eC7T3j4b1c2h3e4b",
					"vault_key_id":    "tenant-1/tron_mainnet/m/44'/195'/0'/0/0",
					"derivation_path": "m/44'/195'/0'/0/0",
				},
			},
			wantResult: domaincrypto.WalletDerivation{
				Address:        "TJBwE3H6y2H3c4x1j3eC7T3j4b1c2h3e4b",
				VaultKeyID:     "tenant-1/tron_mainnet/m/44'/195'/0'/0/0",
				DerivationPath: "m/44'/195'/0'/0/0",
			},
		},
		{
			name:          "unsupported network",
			network:       "ethereum_mainnet",
			wantErrPrefix: "TRON signer cannot derive family evm",
		},
		{
			name:          "vault error",
			network:       "tron_mainnet",
			mockStatus:    http.StatusForbidden,
			mockResponse:  map[string]any{"errors": []string{"permission denied"}},
			wantErrPrefix: "vault generate address: vault HTTP 403",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/tron/tenants/"+testTenantID+"/wallets/derive", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "test-token", r.Header.Get("X-Vault-Token"))

				var req map[string]string
				err := json.NewDecoder(r.Body).Decode(&req)
				require.NoError(t, err)
				assert.Equal(t, tt.network, req["network"])
				assert.Equal(t, "m/44'/195'/0'/0/0", req["derivation_path"])

				w.WriteHeader(tt.mockStatus)
				_ = json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			signer := newTestSigner(t, server.URL, "test-token")

			family := domaincrypto.ChainFamilyTRON
			identifier := "mainnet"
			if tt.network == "ethereum_mainnet" {
				family = domaincrypto.ChainFamilyEVM
			}
			got, err := signer.DeriveAddress(context.Background(), domaincrypto.NetworkRef{Family: family, NetworkIdentifier: identifier}, "m/44'/195'/0'/0/0")
			if tt.wantErrPrefix != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrPrefix)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantResult, got)
			}
		})
	}
}

func TestVaultSigner_SignPolicyEnvelope(t *testing.T) {
	tests := []struct {
		name          string
		mockStatus    int
		mockResponse  any
		wantSignedTx  domaincrypto.SignedTxEnvelope
		wantErrPrefix string
	}{
		{
			name:       "success",
			mockStatus: http.StatusOK,
			mockResponse: map[string]any{
				"data": map[string]any{
					"signed_envelope": map[string]any{
						"family": "tron", "schemaVersion": 1,
						"unsignedFingerprint": "fingerprint", "generation": 1, "txHash": "tx-1",
						"payload": map[string]any{"txID": "tx-1", "signature": []string{"signature-hex"}},
					},
				},
			},
			wantSignedTx: domaincrypto.SignedTxEnvelope{
				Family: domaincrypto.ChainFamilyTRON, SchemaVersion: 1,
				UnsignedFingerprint: "fingerprint", Generation: 1, TxHash: "tx-1",
			},
		},
		{
			name:       "generation mismatch",
			mockStatus: http.StatusOK,
			mockResponse: map[string]any{
				"data": map[string]any{
					"signed_envelope": map[string]any{
						"family": "tron", "schemaVersion": 1,
						"unsignedFingerprint": "fingerprint", "generation": 2, "txHash": "tx-1",
						"payload": map[string]any{"txID": "tx-1", "signature": []string{"signature-hex"}},
					},
				},
			},
			wantErrPrefix: "vault sign TRON envelope returned inconsistent result",
		},
		{
			name:          "vault error",
			mockStatus:    http.StatusBadRequest,
			mockResponse:  map[string]any{"errors": []string{"invalid address"}},
			wantErrPrefix: "vault sign TRON envelope: vault HTTP 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/tron/tenants/"+testTenantID+"/tx/sign", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "test-token", r.Header.Get("X-Vault-Token"))

				var req domaincrypto.PolicySigningRequest
				err := json.NewDecoder(r.Body).Decode(&req)
				require.NoError(t, err)
				assert.Equal(t, "auth.jws", req.AuthorizationJWS)
				assert.Equal(t, "fingerprint", req.UnsignedEnvelope.UnsignedFingerprint)

				w.WriteHeader(tt.mockStatus)
				_ = json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			signer := newTestSigner(t, server.URL, "test-token")

			signedTx, err := signer.Sign(context.Background(), domaincrypto.PolicySigningRequest{
				AuthorizationJWS: "auth.jws",
				UnsignedEnvelope: domaincrypto.UnsignedTxEnvelope{
					Family: domaincrypto.ChainFamilyTRON, SchemaVersion: 1,
					UnsignedFingerprint: "fingerprint", Generation: 1, Payload: json.RawMessage(`{"txID":"tx-1"}`),
				},
			})
			if tt.wantErrPrefix != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrPrefix)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantSignedTx.Family, signedTx.Family)
				assert.Equal(t, tt.wantSignedTx.TxHash, signedTx.TxHash)
				assert.Equal(t, tt.wantSignedTx.UnsignedFingerprint, signedTx.UnsignedFingerprint)
				assert.Equal(t, tt.wantSignedTx.Generation, signedTx.Generation)
			}
		})
	}
}

func TestVaultSigner_RetriesServerErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []string{"temporary"}})
			return
		}

		assert.Equal(t, "/v1/tron/tenants/"+testTenantID+"/wallets/derive", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]string{
				"address":         "TRetryAddress",
				"vault_key_id":    "tenant-1/tron_mainnet/m/44'/195'/0'/0/0",
				"derivation_path": "m/44'/195'/0'/0/0",
			},
		})
	}))
	defer server.Close()

	signer := newTestSigner(t, server.URL, "test-token")

	got, err := signer.DeriveAddress(context.Background(), domaincrypto.NetworkRef{Family: domaincrypto.ChainFamilyTRON, NetworkIdentifier: "mainnet"}, "m/44'/195'/0'/0/0")
	require.NoError(t, err)
	assert.Equal(t, "TRetryAddress", got.Address)
	assert.Equal(t, 2, attempts)
}

func TestVaultSigner_CheckReady(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		response     map[string]any
		wantErrMatch string
	}{
		{name: "configured", status: http.StatusOK, response: map[string]any{"data": map[string]any{"schema_version": "tron_tenant_signing_config.v3", "authorization_keyring_configured": true, "authorization_key_ids": []string{"transit/metapus-signing-auth-" + testTenantID + "/v1"}, "approval_public_keys_configured": true, "hd_seed_configured": true, "sweep_policy_networks": []string{"tron_nile"}}}},
		{name: "mount missing", status: http.StatusNotFound, response: map[string]any{"errors": []string{"route entry not found"}}, wantErrMatch: "HTTP 404"},
		{name: "incomplete config", status: http.StatusOK, response: map[string]any{"data": map[string]any{"schema_version": "tron_tenant_signing_config.v3", "authorization_keyring_configured": true, "authorization_key_ids": []string{"transit/metapus-signing-auth-" + testTenantID + "/v1"}, "approval_public_keys_configured": true, "hd_seed_configured": false, "sweep_policy_networks": []string{"tron_nile"}}}, wantErrMatch: "configuration is incomplete"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/tron/tenants/"+testTenantID+"/config/readiness", r.URL.Path)
				assert.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(tt.status)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()
			signer := newTestSigner(t, server.URL, "test-token")
			err := signer.CheckReady(context.Background(), domaincrypto.NetworkRef{Family: domaincrypto.ChainFamilyTRON, NetworkIdentifier: "nile"}, "transit/metapus-signing-auth-"+testTenantID+"/v1")
			if tt.wantErrMatch == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErrMatch)
			}
		})
	}
}

func newTestSigner(t *testing.T, addr, token string) *VaultSigner {
	t.Helper()
	client, err := vaultclient.New(vaultclient.Config{Addr: addr, Token: token})
	require.NoError(t, err)
	signer, err := New(Config{Client: client, TenantID: testTenantID})
	require.NoError(t, err)
	return signer
}
