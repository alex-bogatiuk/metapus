package backend

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
	"vault-plugin-secrets-tron/internal/json"
	"vault-plugin-secrets-tron/internal/tron"

	"github.com/fbsobreira/gotron-sdk/pkg/address"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/core"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestTenantWalletDeriveIsHDAndIdempotent(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, seed := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")
	require.NotEmpty(t, keys.authPub)

	first := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/0")
	second := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/0")
	otherIndex := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/1")

	expected, err := tron.DeriveWalletFromSeed(seed, "m/44'/195'/0'/0/0")
	require.NoError(t, err)
	assert.Equal(t, expected.Address, first["address"])
	assert.Equal(t, first, second)
	assert.NotEqual(t, first["address"], otherIndex["address"])
	assert.Equal(t, "00000000-0000-0000-0000-000000000001/tron_mainnet/m/44'/195'/0'/0/0", first["vault_key_id"])
}

func TestConfigRejectsLegacySinglePublicKey(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys := newBackendSigningKeys(t)
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "tenants/00000000-0000-0000-0000-000000000001/config/authorization",
		Storage:   storage,
		Data: map[string]any{
			"public_key": encodePublicKey(keys.authPub),
			"hd_seed":    base64.RawURLEncoding.EncodeToString(testSeed()),
		},
	})
	require.NoError(t, err)
	require.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "schema_version")
}

func TestConfigRejectsDuplicateSigningKeys(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys := newBackendSigningKeys(t)
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "tenants/00000000-0000-0000-0000-000000000001/config/authorization",
		Storage:   storage,
		Data: map[string]any{
			"schema_version":             tenantSigningConfigSchemaV3,
			"authorization_keyring":      map[string]any{testAuthorizationKID("00000000-0000-0000-0000-000000000001"): encodePublicKey(keys.authPub)},
			"policy_public_key":          encodePublicKey(keys.authPub),
			"intent_public_key":          encodePublicKey(keys.intentPub),
			"risk_assessment_public_key": encodePublicKey(keys.riskPub),
			"policy_decision_public_key": encodePublicKey(keys.decisionPub),
			"admin_approval_public_key":  encodePublicKey(keys.adminApprovalPub),
			"risk_approval_public_key":   encodePublicKey(keys.riskApprovalPub),
			"hd_seed":                    base64.RawURLEncoding.EncodeToString(testSeed()),
		},
	})
	require.NoError(t, err)
	require.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "duplicates")
}

func TestConfigRejectsAuthorizationKeyForDifferentTenant(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys := newBackendSigningKeys(t)
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "tenants/00000000-0000-0000-0000-000000000001/config/authorization",
		Storage:   storage,
		Data: map[string]any{
			"schema_version":             tenantSigningConfigSchemaV3,
			"authorization_keyring":      map[string]any{testAuthorizationKID("00000000-0000-0000-0000-000000000002"): encodePublicKey(keys.authPub)},
			"policy_public_key":          encodePublicKey(keys.policyPub),
			"intent_public_key":          encodePublicKey(keys.intentPub),
			"risk_assessment_public_key": encodePublicKey(keys.riskPub),
			"policy_decision_public_key": encodePublicKey(keys.decisionPub),
			"admin_approval_public_key":  encodePublicKey(keys.adminApprovalPub),
			"risk_approval_public_key":   encodePublicKey(keys.riskApprovalPub),
			"hd_seed":                    base64.RawURLEncoding.EncodeToString(testSeed()),
		},
	})
	require.NoError(t, err)
	require.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "different tenant")
}

func TestSignRejectsTenantPathMismatch(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000002")

	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	auth := testAuthorization("auth-tenant-mismatch", "00000000-0000-0000-0000-000000000001", source.Address, destination.Address, "00000000-0000-0000-0000-000000000001/tron_mainnet/m/44'/195'/0'/0/0", 1_000_000, time.Now().UTC().Add(10*time.Minute))
	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, auth.AmountMinorUnits, time.Now().UTC().Add(5*time.Minute))
	bindBackendFingerprint(t, &auth, unsigned)

	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "tenants/00000000-0000-0000-0000-000000000002/tx/sign",
		Storage:   storage,
		Data: map[string]any{
			"authorization_jws": signAuthorizationWithKID(t, auth, keys.authPriv, testAuthorizationKID("00000000-0000-0000-0000-000000000002")),
			"unsigned_envelope": unsignedEnvelopeRequestData(unsigned, auth.UnsignedFingerprint),
		},
	})
	require.NoError(t, err)
	require.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "tenant_id does not match path tenant_id")
}

func TestSignIdempotencyRejectsDifferentTxID(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")

	source := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/7")
	destination := mustGenerateWallet(t)
	now := time.Now().UTC()
	amount := int64(1_000_000)
	auth := testAuthorization("auth-idempotent", "00000000-0000-0000-0000-000000000001", source["address"], destination.Address, source["vault_key_id"], amount, now.Add(10*time.Minute))
	approvalBundle := attachBackendApprovalBundle(t, &auth, keys, now)
	unsigned1 := makeNativeUnsignedTx(t, source["address"], destination.Address, amount, now.Add(5*time.Minute))
	bindBackendFingerprint(t, &auth, unsigned1)
	authJWS := signAuthorization(t, auth, keys.authPriv)
	firstResp := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", authJWS, approvalBundle, unsigned1)
	require.False(t, firstResp.IsError(), firstResp.Error())
	assert.Equal(t, unsigned1.TxID, firstResp.Data["tx_id"])

	unsigned2 := makeNativeUnsignedTx(t, source["address"], destination.Address, amount, now.Add(6*time.Minute))
	require.NotEqual(t, unsigned1.TxID, unsigned2.TxID)
	secondResp := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", authJWS, approvalBundle, unsigned2)
	require.True(t, secondResp.IsError())
	assert.Contains(t, secondResp.Error().Error(), "fingerprint")
}

func TestSignConcurrentDifferentTxIDOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")

	source := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/8")
	destination := mustGenerateWallet(t)
	now := time.Now().UTC()
	amount := int64(1_000_000)
	auth := testAuthorization("auth-concurrent", "00000000-0000-0000-0000-000000000001", source["address"], destination.Address, source["vault_key_id"], amount, now.Add(10*time.Minute))
	approvalBundle := attachBackendApprovalBundle(t, &auth, keys, now)
	unsigned1 := makeNativeUnsignedTx(t, source["address"], destination.Address, amount, now.Add(5*time.Minute))
	unsigned2 := makeNativeUnsignedTx(t, source["address"], destination.Address, amount, now.Add(6*time.Minute))
	bindBackendFingerprint(t, &auth, unsigned1)
	authJWS := signAuthorization(t, auth, keys.authPriv)
	require.NotEqual(t, unsigned1.TxID, unsigned2.TxID)

	responses := make(chan *logical.Response, 2)
	go func() {
		responses <- signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", authJWS, approvalBundle, unsigned1)
	}()
	go func() {
		responses <- signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", authJWS, approvalBundle, unsigned2)
	}()

	respA := <-responses
	respB := <-responses
	successes := 0
	failures := 0
	for _, resp := range []*logical.Response{respA, respB} {
		if resp.IsError() {
			failures++
			assert.True(t, strings.Contains(resp.Error().Error(), "approval intent already consumed") || strings.Contains(resp.Error().Error(), "fingerprint"))
			continue
		}
		successes++
		assert.NotEmpty(t, resp.Data["tx_id"])
		assert.NotNil(t, resp.Data["signed_envelope"])
		assert.True(t, resp.Data["signed_envelope"].(tron.SignedEnvelope).Payload.Visible)
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, failures)
}

func TestSignExactRetryReturnsStoredTransaction(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")
	source := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/12")
	destination := mustGenerateWallet(t)
	now := time.Now().UTC()
	auth := testAuthorization("auth-exact-retry", "00000000-0000-0000-0000-000000000001", source["address"], destination.Address, source["vault_key_id"], 1_000_000, now.Add(10*time.Minute))
	bundle := attachBackendApprovalBundle(t, &auth, keys, now)
	unsigned := makeNativeUnsignedTx(t, source["address"], destination.Address, auth.AmountMinorUnits, now.Add(5*time.Minute))
	bindBackendFingerprint(t, &auth, unsigned)
	bindBackendFingerprint(t, &auth, unsigned)
	authJWS := signAuthorization(t, auth, keys.authPriv)

	first := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", authJWS, bundle, unsigned)
	second := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", authJWS, bundle, unsigned)
	require.False(t, first.IsError(), first.Error())
	require.False(t, second.IsError(), second.Error())
	assert.Equal(t, first.Data["signed_envelope"], second.Data["signed_envelope"])
	firstEnvelope := first.Data["signed_envelope"].(tron.SignedEnvelope)
	assert.Equal(t, 1, firstEnvelope.Generation)
	assert.Empty(t, firstEnvelope.NetworkSequence)

	changedBundle := bundle
	changedBundle.RiskAssessmentJWS += "x"
	changed := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", authJWS, changedBundle, unsigned)
	require.True(t, changed.IsError())
	assert.Contains(t, changed.Error().Error(), "bundle digest")
}

func TestApprovalIntentCannotBeReusedWithNewAuthorization(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")
	source := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/13")
	destination := mustGenerateWallet(t)
	now := time.Now().UTC()
	auth := testAuthorization("auth-original", "00000000-0000-0000-0000-000000000001", source["address"], destination.Address, source["vault_key_id"], 1_000_000, now.Add(10*time.Minute))
	bundle := attachBackendApprovalBundle(t, &auth, keys, now)
	unsigned := makeNativeUnsignedTx(t, source["address"], destination.Address, auth.AmountMinorUnits, now.Add(5*time.Minute))
	bindBackendFingerprint(t, &auth, unsigned)
	require.False(t, signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", signAuthorization(t, auth, keys.authPriv), bundle, unsigned).IsError())

	auth.AuthorizationID = "auth-replay"
	auth.Attempt = 2
	replayed := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", signAuthorization(t, auth, keys.authPriv), bundle, unsigned)
	require.True(t, replayed.IsError())
	assert.Contains(t, replayed.Error().Error(), "approval intent already consumed")
}

func TestSignRejectsAuthorizationKIDOutsideKeyring(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")
	source := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/14")
	destination := mustGenerateWallet(t)
	now := time.Now().UTC()
	auth := testAuthorization("auth-unknown-kid", "00000000-0000-0000-0000-000000000001", source["address"], destination.Address, source["vault_key_id"], 1_000_000, now.Add(10*time.Minute))
	bundle := attachBackendApprovalBundle(t, &auth, keys, now)
	unsigned := makeNativeUnsignedTx(t, source["address"], destination.Address, auth.AmountMinorUnits, now.Add(5*time.Minute))
	unknownKID := "transit/metapus-signing-auth-00000000-0000-0000-0000-000000000001/v2"
	resp := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", signAuthorizationWithKID(t, auth, keys.authPriv, unknownKID), bundle, unsigned)
	require.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "kid is not allowed")
}

func TestSweepRequiresAllowlistedDerivedTenantDestination(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")
	source := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/15")
	destination := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/16")
	configureSweepDestinations(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", []string{destination["address"]})
	now := time.Now().UTC()
	auth := testAuthorization("auth-sweep", "00000000-0000-0000-0000-000000000001", source["address"], destination["address"], source["vault_key_id"], 1_000_000, now.Add(10*time.Minute))
	auth.Purpose = "sweep"
	auth.MerchantID = nil
	auth.BasisType = "CryptoSweepAttempt"
	auth.ApprovalBundleDigest = ""
	unsigned := makeNativeUnsignedTx(t, source["address"], destination["address"], auth.AmountMinorUnits, now.Add(5*time.Minute))
	bindBackendFingerprint(t, &auth, unsigned)
	allowed := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", signAuthorization(t, auth, keys.authPriv), tron.ApprovalBundle{}, unsigned)
	require.False(t, allowed.IsError(), allowed.Error())
	replacement := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/18")
	configureSweepDestinations(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", []string{replacement["address"]})
	revokedRetry := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", signAuthorization(t, auth, keys.authPriv), tron.ApprovalBundle{}, unsigned)
	require.True(t, revokedRetry.IsError())
	assert.Contains(t, revokedRetry.Error().Error(), "sweep policy denied")

	external := mustGenerateWallet(t)
	auth.AuthorizationID = "auth-sweep-external"
	auth.BasisID = "external-basis"
	auth.DestinationAddress = external.Address
	unsigned = makeNativeUnsignedTx(t, source["address"], external.Address, auth.AmountMinorUnits, now.Add(5*time.Minute))
	bindBackendFingerprint(t, &auth, unsigned)
	denied := signTx(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", signAuthorization(t, auth, keys.authPriv), tron.ApprovalBundle{}, unsigned)
	require.True(t, denied.IsError())
	assert.Contains(t, denied.Error().Error(), "sweep policy denied")

	configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000002")
	otherTenant := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000002", "tron_mainnet", "m/44'/195'/0'/0/17")
	resp, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.UpdateOperation, Path: "tenants/00000000-0000-0000-0000-000000000001/config/sweep-destinations", Storage: storage, Data: map[string]any{
		"schema_version": sweepDestinationsSchemaV1, "version": "v2", "destinations": map[string]any{"tron_mainnet": []string{otherTenant["address"]}},
	}})
	require.NoError(t, err)
	require.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "not a derived tenant wallet")
}

func TestNativeFundingRequiresManagedDestinationInSameTenantAndNetwork(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")
	configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000002")
	source := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/21")
	managed := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/22")
	foreign := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000002", "tron_mainnet", "m/44'/195'/0'/0/23")
	external := mustGenerateWallet(t)
	now := time.Now().UTC()

	for _, test := range []struct {
		name        string
		destination string
		wantError   bool
	}{
		{name: "managed", destination: managed["address"]},
		{name: "external", destination: external.Address, wantError: true},
		{name: "foreign tenant", destination: foreign["address"], wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth := testAuthorization("auth-funding-"+strings.ReplaceAll(test.name, " ", "-"), "00000000-0000-0000-0000-000000000001", source["address"], test.destination, source["vault_key_id"], 2_000_000, now.Add(10*time.Minute))
			auth.Purpose = "native_funding"
			auth.MerchantID = nil
			auth.BasisType = "CryptoNativeFunding"
			auth.ApprovalBundleDigest = ""
			unsigned := makeNativeUnsignedTx(t, source["address"], test.destination, auth.AmountMinorUnits, now.Add(5*time.Minute))
			bindBackendFingerprint(t, &auth, unsigned)
			resp := signTx(t, ctx, b, storage, auth.TenantID, signAuthorization(t, auth, keys.authPriv), tron.ApprovalBundle{}, unsigned)
			if test.wantError {
				require.True(t, resp.IsError())
				assert.Contains(t, resp.Error().Error(), "native funding policy denied")
				return
			}
			require.False(t, resp.IsError(), resp.Error())
		})
	}
}

func testBackend(t *testing.T, ctx context.Context) (logical.Backend, logical.Storage) {
	t.Helper()
	storage := &logical.InmemStorage{}
	b, err := Factory(ctx, &logical.BackendConfig{StorageView: storage})
	require.NoError(t, err)
	return b, storage
}

type backendSigningKeys struct {
	authPub           ed25519.PublicKey
	authPriv          ed25519.PrivateKey
	policyPub         ed25519.PublicKey
	policyPriv        ed25519.PrivateKey
	intentPub         ed25519.PublicKey
	intentPriv        ed25519.PrivateKey
	riskPub           ed25519.PublicKey
	riskPriv          ed25519.PrivateKey
	decisionPub       ed25519.PublicKey
	decisionPriv      ed25519.PrivateKey
	adminApprovalPub  ed25519.PublicKey
	adminApprovalPriv ed25519.PrivateKey
	riskApprovalPub   ed25519.PublicKey
	riskApprovalPriv  ed25519.PrivateKey
}

func configureTenant(t *testing.T, ctx context.Context, b logical.Backend, storage logical.Storage, tenantID string) (backendSigningKeys, []byte) {
	t.Helper()
	keys := newBackendSigningKeys(t)
	seed := testSeed()

	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "tenants/" + tenantID + "/config/authorization",
		Storage:   storage,
		Data: map[string]any{
			"schema_version":             tenantSigningConfigSchemaV3,
			"authorization_keyring":      map[string]any{testAuthorizationKID(tenantID): encodePublicKey(keys.authPub)},
			"policy_public_key":          encodePublicKey(keys.policyPub),
			"intent_public_key":          encodePublicKey(keys.intentPub),
			"risk_assessment_public_key": encodePublicKey(keys.riskPub),
			"policy_decision_public_key": encodePublicKey(keys.decisionPub),
			"admin_approval_public_key":  encodePublicKey(keys.adminApprovalPub),
			"risk_approval_public_key":   encodePublicKey(keys.riskApprovalPub),
			"hd_seed":                    base64.RawURLEncoding.EncodeToString(seed),
		},
	})
	require.NoError(t, err)
	require.False(t, resp.IsError(), resp.Error())
	return keys, seed
}

func newBackendSigningKeys(t *testing.T) backendSigningKeys {
	t.Helper()
	authPub, authPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	policyPub, policyPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	intentPub, intentPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	riskPub, riskPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	decisionPub, decisionPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	adminPub, adminPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	riskApprovalPub, riskApprovalPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return backendSigningKeys{
		authPub:           authPub,
		authPriv:          authPriv,
		policyPub:         policyPub,
		policyPriv:        policyPriv,
		intentPub:         intentPub,
		intentPriv:        intentPriv,
		riskPub:           riskPub,
		riskPriv:          riskPriv,
		decisionPub:       decisionPub,
		decisionPriv:      decisionPriv,
		adminApprovalPub:  adminPub,
		adminApprovalPriv: adminPriv,
		riskApprovalPub:   riskApprovalPub,
		riskApprovalPriv:  riskApprovalPriv,
	}
}

func encodePublicKey(key ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(key)
}

func testSeed() []byte {
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return seed
}

func deriveWallet(t *testing.T, ctx context.Context, b logical.Backend, storage logical.Storage, tenantID, network, derivationPath string) map[string]string {
	t.Helper()
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "tenants/" + tenantID + "/wallets/derive",
		Storage:   storage,
		Data: map[string]any{
			"network":         network,
			"derivation_path": derivationPath,
		},
	})
	require.NoError(t, err)
	require.False(t, resp.IsError(), resp.Error())
	return map[string]string{
		"address":         resp.Data["address"].(string),
		"vault_key_id":    resp.Data["vault_key_id"].(string),
		"derivation_path": resp.Data["derivation_path"].(string),
	}
}

func signTx(t *testing.T, ctx context.Context, b logical.Backend, storage logical.Storage, tenantID, authJWS string, approvalBundle tron.ApprovalBundle, unsigned tron.UnsignedTransaction) *logical.Response {
	t.Helper()
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "tenants/" + tenantID + "/tx/sign",
		Storage:   storage,
		Data: map[string]any{
			"authorization_jws": authJWS,
			"approval_bundle":   approvalBundleRequestData(t, approvalBundle),
			"unsigned_envelope": unsignedEnvelopeRequestData(unsigned, mustUnsignedFingerprint(t, unsigned)),
		},
	})
	require.NoError(t, err)
	return resp
}

func TestSignRejectsIncompleteUnsignedEnvelope(t *testing.T) {
	ctx := context.Background()
	b, storage := testBackend(t, ctx)
	keys, _ := configureTenant(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001")

	source := deriveWallet(t, ctx, b, storage, "00000000-0000-0000-0000-000000000001", "tron_mainnet", "m/44'/195'/0'/0/9")
	destination := mustGenerateWallet(t)
	now := time.Now().UTC()
	auth := testAuthorization("auth-incomplete-envelope", "00000000-0000-0000-0000-000000000001", source["address"], destination.Address, source["vault_key_id"], 1_000_000, now.Add(10*time.Minute))
	approvalBundle := attachBackendApprovalBundle(t, &auth, keys, now)
	unsigned := makeNativeUnsignedTx(t, source["address"], destination.Address, 1_000_000, now.Add(5*time.Minute))
	unsigned.Visible = false
	bindBackendFingerprint(t, &auth, unsigned)

	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "tenants/00000000-0000-0000-0000-000000000001/tx/sign",
		Storage:   storage,
		Data: map[string]any{
			"authorization_jws": signAuthorization(t, auth, keys.authPriv),
			"approval_bundle":   approvalBundleRequestData(t, approvalBundle),
			"unsigned_envelope": unsignedEnvelopeRequestData(unsigned, auth.UnsignedFingerprint),
		},
	})
	require.NoError(t, err)
	require.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "visible=true")
}

func mustGenerateWallet(t *testing.T) *tron.WalletData {
	t.Helper()
	wallet, err := tron.GenerateWallet()
	require.NoError(t, err)
	return wallet
}

func testAuthorization(authID, tenantID, source, destination, vaultKeyID string, amount int64, expiresAt time.Time) tron.SigningAuthorization {
	merchantID := "merchant-1"
	return tron.SigningAuthorization{
		SchemaVersion:      tron.SigningAuthorizationSchemaVersion,
		AuthorizationID:    authID,
		TenantID:           tenantID,
		MerchantID:         &merchantID,
		Purpose:            "withdrawal",
		NetworkID:          "00000000-0000-0000-0000-000000000010",
		NetworkCode:        "TRON-MAINNET",
		ChainFamily:        "tron",
		NetworkIdentifier:  "mainnet",
		SourceAddress:      source,
		FeePayerAddress:    source,
		VaultKeyID:         vaultKeyID,
		DestinationAddress: destination,
		AssetStandard:      tron.AssetStandardNative,
		AmountMinorUnits:   tron.NewRawAmountFromInt64(amount),
		MaxNetworkFeeMinor: tron.NewRawAmountFromInt64(100_000_000),
		BasisType:          "WithdrawalRequest",
		BasisID:            authID + "-basis",
		Attempt:            1,
		Generation:         1,
		ExpiresAt:          expiresAt,
		PolicyVersion:      tron.SigningAuthorizationPolicyVersion,
	}
}

func mustUnsignedFingerprint(t *testing.T, unsigned tron.UnsignedTransaction) string {
	t.Helper()
	fingerprint, err := tron.UnsignedFingerprint(unsigned)
	require.NoError(t, err)
	return fingerprint
}

func bindBackendFingerprint(t *testing.T, auth *tron.SigningAuthorization, unsigned tron.UnsignedTransaction) {
	t.Helper()
	auth.UnsignedFingerprint = mustUnsignedFingerprint(t, unsigned)
}

func unsignedEnvelopeRequestData(unsigned tron.UnsignedTransaction, fingerprint string) map[string]any {
	return map[string]any{
		"family":              "tron",
		"schemaVersion":       1,
		"unsignedFingerprint": fingerprint,
		"generation":          1,
		"payload": map[string]any{
			"txID": unsigned.TxID, "raw_data_hex": unsigned.RawDataHex,
			"raw_data": unsigned.RawData, "visible": unsigned.Visible,
		},
	}
}

func signAuthorization(t *testing.T, auth tron.SigningAuthorization, privateKey ed25519.PrivateKey) string {
	return signAuthorizationWithKID(t, auth, privateKey, testAuthorizationKID(auth.TenantID))
}

func signAuthorizationWithKID(t *testing.T, auth tron.SigningAuthorization, privateKey ed25519.PrivateKey, kid string) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": kid})
	require.NoError(t, err)
	payload, err := json.Marshal(auth)
	require.NoError(t, err)

	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := encodedHeader + "." + encodedPayload
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func testAuthorizationKID(tenantID string) string {
	return "transit/metapus-signing-auth-" + tenantID + "/v1"
}

func configureSweepDestinations(t *testing.T, ctx context.Context, b logical.Backend, storage logical.Storage, tenantID, network string, addresses []string) {
	t.Helper()
	resp, err := b.HandleRequest(ctx, &logical.Request{Operation: logical.UpdateOperation, Path: "tenants/" + tenantID + "/config/sweep-destinations", Storage: storage, Data: map[string]any{
		"schema_version": sweepDestinationsSchemaV1, "version": "v1", "destinations": map[string]any{network: addresses},
	}})
	require.NoError(t, err)
	require.False(t, resp.IsError(), resp.Error())
}

func approvalBundleRequestData(t *testing.T, bundle tron.ApprovalBundle) map[string]any {
	t.Helper()
	payload, err := json.Marshal(bundle)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(payload, &out))
	return out
}

func attachBackendApprovalBundle(t *testing.T, auth *tron.SigningAuthorization, keys backendSigningKeys, now time.Time) tron.ApprovalBundle {
	t.Helper()
	policy := tron.ApprovalPolicyBundle{
		SchemaVersion:             "approval_policy.v1",
		PolicyVersion:             "money-movement-v1.0",
		TenantID:                  auth.TenantID,
		DefaultRequiredPrincipals: []tron.ApprovalPrincipal{tron.ApprovalPrincipalRisk},
		EffectiveFrom:             now.Add(-time.Hour),
		ExpiresAt:                 now.Add(time.Hour),
	}
	policyJWS := signBackendPayload(t, policy, keys.policyPriv)
	policyDigest := backendJWSPayloadDigest(t, policyJWS)
	intent := tron.MoneyMovementIntent{
		SchemaVersion:      "money_movement_intent.v1",
		IntentID:           auth.AuthorizationID + "-intent",
		TenantID:           auth.TenantID,
		Purpose:            auth.Purpose,
		BasisType:          auth.BasisType,
		BasisID:            auth.BasisID,
		MerchantID:         auth.MerchantID,
		NetworkCode:        auth.NetworkCode,
		TokenID:            "token-1",
		TokenStandard:      auth.AssetStandard,
		ContractAddress:    auth.ContractAddress,
		AmountMinorUnits:   auth.AmountMinorUnits,
		DestinationAddress: auth.DestinationAddress,
		AddressID:          "address-1",
		PolicyVersion:      policy.PolicyVersion,
		PolicyDigest:       policyDigest,
		ExpiresAt:          auth.ExpiresAt,
	}
	intentJWS := signBackendPayload(t, intent, keys.intentPriv)
	intentDigest := backendJWSPayloadDigest(t, intentJWS)
	risk := tron.RiskAssessment{
		SchemaVersion: "risk_assessment.v1",
		TenantID:      auth.TenantID,
		IntentDigest:  intentDigest,
		RiskLevel:     "low",
		RiskScore:     1,
		Signals:       []string{"test"},
		AssessedAt:    now,
		ExpiresAt:     now.Add(time.Hour),
	}
	riskJWS := signBackendPayload(t, risk, keys.riskPriv)
	riskDigest := backendJWSPayloadDigest(t, riskJWS)
	decision := tron.ApprovalPolicyDecision{
		SchemaVersion:      "approval_policy_decision.v1",
		TenantID:           auth.TenantID,
		IntentDigest:       intentDigest,
		PolicyVersion:      policy.PolicyVersion,
		PolicyDigest:       policyDigest,
		RequiredPrincipals: []tron.ApprovalPrincipal{tron.ApprovalPrincipalRisk},
		DecidedAt:          now,
		ExpiresAt:          now.Add(time.Hour),
	}
	decisionJWS := signBackendPayload(t, decision, keys.decisionPriv)
	decisionDigest := backendJWSPayloadDigest(t, decisionJWS)
	approval := tron.MoneyMovementApproval{
		SchemaVersion:        "money_movement_approval.v1",
		ApprovalID:           auth.AuthorizationID + "-risk-approval",
		TenantID:             auth.TenantID,
		IntentDigest:         intentDigest,
		PrincipalType:        tron.ApprovalPrincipalRisk,
		PrincipalID:          "risk-engine",
		PolicyVersion:        policy.PolicyVersion,
		PolicyDecisionDigest: decisionDigest,
		RiskAssessmentDigest: riskDigest,
		ApprovedAt:           now,
		ExpiresAt:            now.Add(time.Hour),
	}
	bundle := tron.ApprovalBundle{
		PolicyBundleJWS:   policyJWS,
		IntentJWS:         intentJWS,
		RiskAssessmentJWS: riskJWS,
		PolicyDecisionJWS: decisionJWS,
		Approvals: []tron.ApprovalBundleApproval{{
			PrincipalType: tron.ApprovalPrincipalRisk,
			JWS:           signBackendPayload(t, approval, keys.riskApprovalPriv),
		}},
	}
	payload, err := json.Marshal(bundle)
	require.NoError(t, err)
	sum := sha256.Sum256(payload)
	auth.ApprovalBundleDigest = hex.EncodeToString(sum[:])
	return bundle
}

func signBackendPayload(t *testing.T, payload any, privateKey ed25519.PrivateKey) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT"})
	require.NoError(t, err)
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := encodedHeader + "." + encodedPayload
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func backendJWSPayloadDigest(t *testing.T, jws string) string {
	t.Helper()
	parts := strings.Split(jws, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func makeNativeUnsignedTx(t *testing.T, source, destination string, rawAmount any, expiresAt time.Time) tron.UnsignedTransaction {
	t.Helper()
	var amount int64
	switch value := rawAmount.(type) {
	case int:
		amount = int64(value)
	case int64:
		amount = value
	case tron.RawAmount:
		var err error
		amount, err = value.Int64Exact()
		require.NoError(t, err)
	default:
		require.FailNowf(t, "unsupported amount", "type %T", rawAmount)
	}
	sourceAddress, err := address.Base58ToAddress(source)
	require.NoError(t, err)
	destinationAddress, err := address.Base58ToAddress(destination)
	require.NoError(t, err)

	transferContract, err := proto.Marshal(&core.TransferContract{
		OwnerAddress: sourceAddress.Bytes(),
		ToAddress:    destinationAddress.Bytes(),
		Amount:       amount,
	})
	require.NoError(t, err)

	raw := &core.TransactionRaw{
		RefBlockBytes: []byte{0x00, 0x01},
		RefBlockHash:  []byte{0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09},
		Expiration:    expiresAt.UnixMilli(),
		Timestamp:     expiresAt.Add(-5 * time.Minute).UnixMilli(),
		Contract: []*core.Transaction_Contract{
			{
				Type: core.Transaction_Contract_TransferContract,
				Parameter: &anypb.Any{
					TypeUrl: "type.googleapis.com/protocol.TransferContract",
					Value:   transferContract,
				},
			},
		},
	}
	rawBytes, err := proto.Marshal(raw)
	require.NoError(t, err)
	rawJSON, err := protojson.Marshal(raw)
	require.NoError(t, err)
	txID := sha256.Sum256(rawBytes)

	return tron.UnsignedTransaction{
		TxID:       hex.EncodeToString(txID[:]),
		RawDataHex: hex.EncodeToString(rawBytes),
		RawData:    rawJSON,
		Visible:    true,
	}
}
