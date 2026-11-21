package authorization_signer

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

const signerTestTenant = "00000000-0000-0000-0000-000000000001"

func TestLocalAuthorizationSignerRejectsProduction(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, err = NewLocal(base64.RawURLEncoding.EncodeToString(privateKey), "local-test", "production")
	require.ErrorContains(t, err, "forbidden")
}

func TestAuthorizationJWSSigningInputIsDeterministic(t *testing.T) {
	auth := testAuthorization()
	kid := "transit/metapus-signing-auth-" + signerTestTenant + "/v7"
	first, err := crypto.BuildAuthorizationJWSSigningInput(auth, kid)
	require.NoError(t, err)
	second, err := crypto.BuildAuthorizationJWSSigningInput(auth, kid)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestLocalAndTransitAuthorizationJWSVerify(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	auth := testAuthorization()

	local, err := NewLocal(base64.RawURLEncoding.EncodeToString(privateKey), "local-test", "development")
	require.NoError(t, err)
	localJWS, err := local.SignAuthorization(context.Background(), signerTestTenant, auth)
	require.NoError(t, err)
	verified := verifyAuthorizationJWS(t, localJWS, publicKey)
	assert.Equal(t, auth.AuthorizationID, verified.AuthorizationID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /v1/transit/keys/metapus-signing-auth-" + signerTestTenant:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"type": "ed25519", "exportable": false, "deletion_allowed": false, "latest_version": 3,
				"keys": map[string]any{"3": map[string]any{"public_key": base64.StdEncoding.EncodeToString(publicKey)}},
			}})
		case http.MethodPost + " /v1/transit/sign/metapus-signing-auth-" + signerTestTenant:
			var request map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			assert.Equal(t, float64(3), request["key_version"])
			input, err := base64.StdEncoding.DecodeString(request["input"].(string))
			require.NoError(t, err)
			signature := ed25519.Sign(privateKey, input)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"signature": "vault:v3:" + base64.StdEncoding.EncodeToString(signature)}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := vaultclient.New(vaultclient.Config{Addr: server.URL, MaxRetries: 1})
	require.NoError(t, err)
	transit, err := NewTransit(TransitConfig{Client: client, Mount: "transit"})
	require.NoError(t, err)
	transitJWS, err := transit.SignAuthorization(context.Background(), signerTestTenant, auth)
	require.NoError(t, err)
	verified = verifyAuthorizationJWS(t, transitJWS, publicKey)
	assert.Equal(t, auth.AuthorizationID, verified.AuthorizationID)
	kid, err := crypto.AuthorizationJWSKeyID(transitJWS)
	require.NoError(t, err)
	assert.Equal(t, "transit/metapus-signing-auth-"+signerTestTenant+"/v3", kid)
}

func TestLocalAuthorizationJWSVerify(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := NewLocal(base64.RawURLEncoding.EncodeToString(privateKey), "local-test", "development")
	require.NoError(t, err)

	auth := testAuthorization()
	jws, err := signer.SignAuthorization(context.Background(), signerTestTenant, auth)
	require.NoError(t, err)
	verified := verifyAuthorizationJWS(t, jws, publicKey)
	assert.Equal(t, auth, verified)
}

func TestTransitAuthorizationSignerFailsClosedOnVersionMismatch(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"type": "ed25519", "latest_version": 3, "keys": map[string]any{"3": map[string]any{"public_key": base64.StdEncoding.EncodeToString(publicKey)}}}})
			return
		}
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		input, err := base64.StdEncoding.DecodeString(request["input"].(string))
		require.NoError(t, err)
		signature := ed25519.Sign(privateKey, input)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"signature": "vault:v2:" + base64.StdEncoding.EncodeToString(signature)}})
	}))
	defer server.Close()
	client, err := vaultclient.New(vaultclient.Config{Addr: server.URL, MaxRetries: 1})
	require.NoError(t, err)
	signer, err := NewTransit(TransitConfig{Client: client})
	require.NoError(t, err)
	_, err = signer.SignAuthorization(context.Background(), signerTestTenant, testAuthorization())
	require.ErrorContains(t, err, "returned key version 2, requested 3")
}

func TestTransitAuthorizationSignerRejectsTenantPathInjection(t *testing.T) {
	client, err := vaultclient.New(vaultclient.Config{Addr: "http://vault-proxy:8100", MaxRetries: 1})
	require.NoError(t, err)
	signer, err := NewTransit(TransitConfig{Client: client})
	require.NoError(t, err)
	_, err = signer.SignAuthorization(context.Background(), signerTestTenant+"/../other", testAuthorization())
	require.ErrorContains(t, err, "canonical UUID")
}

func testAuthorization() crypto.SigningAuthorization {
	return crypto.SigningAuthorization{
		SchemaVersion: crypto.SigningAuthorizationSchemaVersion, AuthorizationID: "auth-test", TenantID: signerTestTenant,
		Purpose: crypto.SigningPurposeSweep, BasisType: "CryptoSweep", BasisID: id.MustParse("00000000-0000-0000-0000-000000000002"),
		Attempt: 1, ExpiresAt: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		NetworkID: id.MustParse("00000000-0000-0000-0000-000000000003"), NetworkCode: "TRON-NILE",
		ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", SourceAddress: "source",
		FeePayerAddress: "source", VaultKeyID: "vault-key", DestinationAddress: "destination",
		AssetStandard: "native", AmountMinorUnits: types.NewRawAmountFromInt64(1), MaxNetworkFeeMinor: types.NewRawAmountFromInt64(1_000_000),
		UnsignedFingerprint: "9a5ba2e42fc6a3e87fcbe97355576361f80f861d4034f4e8ea09544db7d00258",
		PolicyVersion:       crypto.SigningAuthorizationPolicyVersion(crypto.ChainFamilyTRON),
	}
}

func verifyAuthorizationJWS(t *testing.T, jws string, publicKey ed25519.PublicKey) crypto.SigningAuthorization {
	t.Helper()
	var auth crypto.SigningAuthorization
	_, err := crypto.VerifyEdDSAJWS(jws, publicKey, &auth)
	require.NoError(t, err)
	return auth
}
