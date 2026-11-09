package backend

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	"vault-plugin-secrets-evm/internal/evm"
	"vault-plugin-secrets-evm/internal/json"
)

const custodyTenant = "00000000-0000-0000-0000-000000000001"
const custodyMerchantA = "00000000-0000-0000-0000-000000000002"
const custodyMerchantB = "00000000-0000-0000-0000-000000000003"
const custodyNetwork = "11155111"

func custodyRequest(t *testing.T, b logical.Backend, storage logical.Storage, path string, data map[string]any) *logical.Response {
	t.Helper()
	resp, err := b.HandleRequest(t.Context(), &logical.Request{Operation: logical.UpdateOperation, Path: "tenants/" + custodyTenant + "/" + path, Storage: storage, Data: data})
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp
}

func bindCustody(t *testing.T, b logical.Backend, storage logical.Storage, address, merchant, role string) *logical.Response {
	t.Helper()
	return custodyRequest(t, b, storage, "wallets/custody", map[string]any{"network": custodyNetwork, "address": address, "merchant_id": merchant, "role": role})
}

func enableCustody(t *testing.T, b logical.Backend, storage logical.Storage, revision int, networks []string) *logical.Response {
	t.Helper()
	return custodyRequest(t, b, storage, "config/merchant-custody", map[string]any{"schema_version": merchantCustodySchemaV1, "revision": revision, "enforced_networks": networks})
}

func custodyBackend(t *testing.T) (logical.Backend, logical.Storage) {
	t.Helper()
	b, storage := testEVMBackend(t, t.Context())
	configureEVMTenant(t, t.Context(), b, storage)
	return b, storage
}

func deriveCustodyWallet(t *testing.T, b logical.Backend, storage logical.Storage, index int) string {
	t.Helper()
	resp := custodyRequest(t, b, storage, "wallets/derive", map[string]any{"chain_id": custodyNetwork, "derivation_path": fmt.Sprintf("m/44'/60'/0'/0/%d", index)})
	require.False(t, resp.IsError(), resp.Error())
	return resp.Data["address"].(string)
}

func TestMerchantCustodyBindingsAndPolicyAreImmutable(t *testing.T) {
	b, storage := custodyBackend(t)
	address := deriveCustodyWallet(t, b, storage, 10)
	for range 2 {
		resp := bindCustody(t, b, storage, address, custodyMerchantA, "merchant_pool")
		require.False(t, resp.IsError(), resp.Error())
	}
	resp := bindCustody(t, b, storage, address, custodyMerchantB, "merchant_pool")
	require.True(t, resp.IsError())
	require.Contains(t, resp.Error().Error(), "immutable")
	resp = bindCustody(t, b, storage, address, custodyMerchantA, "network_fee")
	require.True(t, resp.IsError())
	resp = custodyRequest(t, b, storage, "wallets/custody", map[string]any{"network": custodyNetwork, "address": deriveCustodyWallet(t, b, storage, 11), "merchant_id": "00000000-0000-0000-0000-000000000000", "role": "merchant_pool"})
	require.True(t, resp.IsError())
	other, err := b.HandleRequest(t.Context(), &logical.Request{Operation: logical.UpdateOperation, Path: "tenants/" + custodyMerchantB + "/wallets/custody", Storage: storage, Data: map[string]any{"network": custodyNetwork, "address": address, "merchant_id": custodyMerchantA, "role": "merchant_pool"}})
	require.NoError(t, err)
	require.True(t, other.IsError(), "a derived tenant address is not proof for another tenant")
	resp = enableCustody(t, b, storage, 1, []string{custodyNetwork})
	require.False(t, resp.IsError(), resp.Error())
	resp = enableCustody(t, b, storage, 1, []string{custodyNetwork})
	require.True(t, resp.IsError())
	resp = enableCustody(t, b, storage, 2, []string{})
	require.True(t, resp.IsError())
	require.Contains(t, resp.Error().Error(), "downgraded")
	ready, err := b.HandleRequest(t.Context(), &logical.Request{Operation: logical.ReadOperation, Path: "tenants/" + custodyTenant + "/config/readiness", Storage: storage})
	require.NoError(t, err)
	require.False(t, ready.IsError(), ready.Error())
	require.Equal(t, true, ready.Data["merchant_custody_supported"])
	require.Equal(t, "aml_signing.v1", ready.Data["aml_signing_schema"])
	require.Equal(t, "money_movement_intent.v2", ready.Data["aml_intent_schema"])
	require.Equal(t, "CryptoAMLWithdrawalLine", ready.Data["aml_source_line_basis"])
	require.Equal(t, []string{custodyNetwork}, ready.Data["merchant_custody_networks"])
}

func TestMerchantCustodyRoutesRejectForeignAndUnboundPrincipal(t *testing.T) {
	b, storage := custodyBackend(t)
	addresses := make([]string, 7)
	for index := range addresses {
		addresses[index] = deriveCustodyWallet(t, b, storage, index+20)
	}
	bindings := []struct{ merchant, role string }{{custodyMerchantA, "merchant_pool"}, {custodyMerchantA, "liquidity_hot"}, {custodyMerchantA, "network_fee"}, {custodyMerchantB, "liquidity_hot"}, {custodyMerchantB, "merchant_pool"}, {"", "treasury"}}
	for i, binding := range bindings {
		resp := bindCustody(t, b, storage, addresses[i], binding.merchant, binding.role)
		require.False(t, resp.IsError(), resp.Error())
	}
	resp := enableCustody(t, b, storage, 1, []string{custodyNetwork})
	require.False(t, resp.IsError(), resp.Error())
	a, bOwner := custodyMerchantA, custodyMerchantB
	for _, tc := range []struct {
		name, purpose string
		from, to      int
		owner         *string
		allowed       bool
	}{
		{"own sweep", "sweep", 0, 1, &a, true}, {"foreign collector", "sweep", 0, 3, &a, false},
		{"foreign claimed owner", "sweep", 0, 1, &bOwner, false}, {"missing owner", "sweep", 0, 1, nil, false},
		{"own funding", "native_funding", 2, 0, &a, true}, {"foreign funding", "native_funding", 2, 4, &a, false},
		{"own activation", "activation", 2, 0, &a, true}, {"foreign activation", "activation", 1, 4, &a, false},
		{"merchant treasury", "treasury_withdrawal", 1, 6, &a, false}, {"platform treasury", "treasury_withdrawal", 5, 6, nil, true},
		{"unbound source", "native_funding", 6, 0, &a, false}, {"unbound destination", "native_funding", 2, 6, &a, false},
		{"managed withdrawal", "withdrawal", 1, 4, &a, false}, {"same source", "native_funding", 2, 2, &a, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := b.(*evmBackend).validateMerchantCustody(context.Background(), storage, custodyTenant, custodyNetwork, tc.purpose, addresses[tc.from], addresses[tc.to], tc.owner)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestMerchantCustodyBlocksPreviouslyCachedForeignSignature(t *testing.T) {
	ctx := t.Context()
	b, storage := testEVMBackend(t, ctx)
	public, private, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	config := validEVMConfig(t)
	kid := "transit/metapus-signing-auth-" + custodyTenant + "/v1"
	config["authorization_keyring"] = map[string]any{kid: base64.RawURLEncoding.EncodeToString(public)}
	resp := custodyRequest(t, b, storage, "config/authorization", config)
	require.False(t, resp.IsError(), resp.Error())
	source := deriveCustodyWallet(t, b, storage, 70)
	destination := deriveCustodyWallet(t, b, storage, 71)
	unsigned := evm.UnsignedTransaction{ChainID: evm.SepoliaChainID, Nonce: "7", MaxPriorityFeePerGasWei: "1000000000", MaxFeePerGasWei: "3000000000", GasLimit: 21000, To: destination, ValueWei: "1000000000000000", Data: "0x", AccessList: gethtypes.AccessList{}}
	fingerprint, err := evm.UnsignedFingerprint(unsigned)
	require.NoError(t, err)
	envelope := evm.UnsignedEnvelope{Family: "evm", SchemaVersion: 1, UnsignedFingerprint: fingerprint, Generation: 1, NetworkSequence: "7", Payload: unsigned}
	merchant := custodyMerchantA
	auth := evm.SigningAuthorization{SchemaVersion: 2, AuthorizationID: "custody-cache-guard", TenantID: custodyTenant, MerchantID: &merchant,
		Purpose: "native_funding", NetworkID: "11155111-0000-4000-8000-000000000001", NetworkCode: "ETHEREUM-SEPOLIA", ChainFamily: "evm", NetworkIdentifier: evm.SepoliaChainID,
		SourceAddress: source, FeePayerAddress: source, VaultKeyID: custodyTenant + "/" + evm.SepoliaChainID + "/m/44'/60'/0'/0/70", DestinationAddress: destination,
		AssetStandard: evm.AssetStandardNative, AmountMinorUnits: evm.RawAmount("1000000000000000"), MaxNetworkFeeMinor: evm.RawAmount("63000000000000"),
		UnsignedFingerprint: fingerprint, BasisType: "CryptoNativeFunding", BasisID: "00000000-0000-0000-0000-000000000002", Attempt: 1, Generation: 1, NetworkSequence: "7", ExpiresAt: time.Now().UTC().Add(time.Minute), PolicyVersion: evm.SigningAuthorizationPolicyVersion}
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": kid})
	require.NoError(t, err)
	payload, err := json.Marshal(auth)
	require.NoError(t, err)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	jws := signingInput + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, []byte(signingInput)))
	encoded, err := json.Marshal(envelope)
	require.NoError(t, err)
	var envelopeMap map[string]any
	require.NoError(t, json.Unmarshal(encoded, &envelopeMap))
	request := map[string]any{"authorization_jws": jws, "unsigned_envelope": envelopeMap}
	first := custodyRequest(t, b, storage, "tx/sign", request)
	require.False(t, first.IsError(), first.Error())
	require.False(t, bindCustody(t, b, storage, source, custodyMerchantA, "network_fee").IsError())
	require.False(t, bindCustody(t, b, storage, destination, custodyMerchantB, "merchant_pool").IsError())
	require.False(t, enableCustody(t, b, storage, 1, []string{custodyNetwork}).IsError())
	replay := custodyRequest(t, b, storage, "tx/sign", request)
	require.True(t, replay.IsError())
	require.Contains(t, replay.Error().Error(), "crosses merchant custody")
}

func TestOwnedCollectorEnrollmentAddsOnlyNewDestinationAndPreservesRevocation(t *testing.T) {
	backend, storage := custodyBackend(t)
	b := backend.(*evmBackend)
	require.False(t, enableCustody(t, backend, storage, 1, []string{custodyNetwork}).IsError())
	collector := deriveCustodyWallet(t, backend, storage, 101)
	deposit := deriveCustodyWallet(t, backend, storage, 102)
	require.False(t, bindCustody(t, backend, storage, collector, custodyMerchantA, "liquidity_hot").IsError())
	require.False(t, bindCustody(t, backend, storage, deposit, custodyMerchantA, "merchant_pool").IsError())
	cfg, err := b.getSweepDestinationsConfig(t.Context(), storage, custodyTenant)
	require.NoError(t, err)
	require.Contains(t, cfg.Destinations[custodyNetwork], collector)
	require.NotContains(t, cfg.Destinations[custodyNetwork], deposit)
	cfg.Destinations[custodyNetwork] = []string{deposit}
	payload, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, storage.Put(t.Context(), &logical.StorageEntry{Key: sweepDestinationsConfigKey(custodyTenant), Value: payload, SealWrap: true}))
	require.False(t, bindCustody(t, backend, storage, collector, custodyMerchantA, "liquidity_hot").IsError())
	cfg, err = b.getSweepDestinationsConfig(t.Context(), storage, custodyTenant)
	require.NoError(t, err)
	require.NotContains(t, cfg.Destinations[custodyNetwork], collector, "an idempotent custody replay cannot restore a revoked destination")
}
