package backend

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"vault-plugin-secrets-evm/internal/evm"

	"github.com/hashicorp/vault/sdk/logical"
	"github.com/stretchr/testify/require"
)

const testTenantID = "00000000-0000-0000-0000-000000000001"

func TestConfigRejectsAnyChainExceptSepolia(t *testing.T) {
	ctx := context.Background()
	b, storage := testEVMBackend(t, ctx)
	data := validEVMConfig(t)
	data["allowed_chain_ids"] = []string{"1"}
	resp, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.UpdateOperation,
		Path: "tenants/" + testTenantID + "/config/authorization", Storage: storage, Data: data})
	require.NoError(t, err)
	require.True(t, resp.IsError())
	require.Contains(t, resp.Error().Error(), "only Sepolia")
}

func TestWalletDerivationIsTenantScopedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	b, storage := testEVMBackend(t, ctx)
	configureEVMTenant(t, ctx, b, storage)
	request := func(path string) *logical.Response {
		resp, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.UpdateOperation,
			Path: "tenants/" + testTenantID + "/wallets/derive", Storage: storage,
			Data: map[string]any{"chain_id": evm.SepoliaChainID, "derivation_path": path}})
		require.NoError(t, err)
		return resp
	}
	first, second := request("m/44'/60'/0'/0/7"), request("m/44'/60'/0'/0/7")
	require.False(t, first.IsError(), first.Error())
	require.False(t, second.IsError(), second.Error())
	require.Equal(t, first.Data["address"], second.Data["address"])
	require.Equal(t, testTenantID+"/"+evm.SepoliaChainID+"/m/44'/60'/0'/0/7", first.Data["vault_key_id"])

	invalid := request("m/44'/195'/0'/0/7")
	require.True(t, invalid.IsError())
}

func TestReadinessDoesNotExposeSeed(t *testing.T) {
	ctx := context.Background()
	b, storage := testEVMBackend(t, ctx)
	configureEVMTenant(t, ctx, b, storage)
	resp, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.ReadOperation,
		Path: "tenants/" + testTenantID + "/config/readiness", Storage: storage})
	require.NoError(t, err)
	require.False(t, resp.IsError(), resp.Error())
	require.Equal(t, true, resp.Data["hd_seed_configured"])
	require.Empty(t, resp.Data["sweep_policy_chain_ids"])
	_, seedExposed := resp.Data["hd_seed"]
	require.False(t, seedExposed)
}

func TestReadinessPublishesOnlyConfiguredSweepPolicyChainIDs(t *testing.T) {
	ctx := context.Background()
	b, storage := testEVMBackend(t, ctx)
	configureEVMTenant(t, ctx, b, storage)
	derive, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.UpdateOperation,
		Path: "tenants/" + testTenantID + "/wallets/derive", Storage: storage,
		Data: map[string]any{"chain_id": evm.SepoliaChainID, "derivation_path": "m/44'/60'/0'/0/9"}})
	require.NoError(t, err)
	require.False(t, derive.IsError(), derive.Error())
	configured, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.UpdateOperation,
		Path: "tenants/" + testTenantID + "/config/sweep-destinations", Storage: storage,
		Data: map[string]any{"schema_version": sweepDestinationsSchemaV1, "version": "test-1",
			"destinations": map[string]any{evm.SepoliaChainID: []string{derive.Data["address"].(string)}}}})
	require.NoError(t, err)
	require.False(t, configured.IsError(), configured.Error())
	ready, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.ReadOperation,
		Path: "tenants/" + testTenantID + "/config/readiness", Storage: storage})
	require.NoError(t, err)
	require.Equal(t, []string{evm.SepoliaChainID}, ready.Data["sweep_policy_chain_ids"])
}

func testEVMBackend(t *testing.T, ctx context.Context) (logical.Backend, logical.Storage) {
	t.Helper()
	storage := &logical.InmemStorage{}
	b, err := Factory(ctx, &logical.BackendConfig{StorageView: storage})
	require.NoError(t, err)
	return b, storage
}

func configureEVMTenant(t *testing.T, ctx context.Context, b logical.Backend, storage logical.Storage) {
	t.Helper()
	resp, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.UpdateOperation,
		Path: "tenants/" + testTenantID + "/config/authorization", Storage: storage, Data: validEVMConfig(t)})
	require.NoError(t, err)
	require.False(t, resp.IsError(), resp.Error())
}

func validEVMConfig(t *testing.T) map[string]any {
	t.Helper()
	keys := make([]ed25519.PublicKey, 7)
	for i := range keys {
		public, _, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		keys[i] = public
	}
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	encode := func(key ed25519.PublicKey) string { return base64.RawURLEncoding.EncodeToString(key) }
	return map[string]any{
		"schema_version": tenantSigningConfigSchemaV1,
		"authorization_keyring": map[string]any{
			"transit/metapus-signing-auth-" + testTenantID + "/v1": encode(keys[0]),
		},
		"policy_public_key": encode(keys[1]), "intent_public_key": encode(keys[2]),
		"risk_assessment_public_key": encode(keys[3]), "policy_decision_public_key": encode(keys[4]),
		"admin_approval_public_key": encode(keys[5]), "risk_approval_public_key": encode(keys[6]),
		"hd_seed": base64.RawURLEncoding.EncodeToString(seed), "allowed_chain_ids": []string{evm.SepoliaChainID},
	}
}
