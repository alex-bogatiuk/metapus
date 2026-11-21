package approval_signer

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

func TestTransitProviderBuildsVerifiableArtifactsWithoutExportingPrivateKeys(t *testing.T) {
	t.Parallel()
	tenantID := id.New().String()
	policyPublic, policyPrivate, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	roleKeys := make(map[string]ed25519.PrivateKey, len(approvalRoles))
	for _, role := range approvalRoles {
		_, privateKey, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		roleKeys[string(role)] = privateKey
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "metapus-approval-"
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		role := strings.TrimSuffix(strings.TrimPrefix(name, prefix), "-"+tenantID)
		privateKey, ok := roleKeys[role]
		if !ok {
			http.Error(w, "unknown role", http.StatusNotFound)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/keys/"):
			publicKey := privateKey.Public().(ed25519.PublicKey)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"type": "ed25519", "exportable": false, "deletion_allowed": false,
				"latest_version": 1,
				"keys":           map[string]any{"1": map[string]any{"public_key": base64.StdEncoding.EncodeToString(publicKey)}},
			}})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/sign/"):
			var request struct {
				Input string `json:"input"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			input, err := base64.StdEncoding.DecodeString(request.Input)
			require.NoError(t, err)
			signature := ed25519.Sign(privateKey, input)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"signature": "vault:v1:" + base64.StdEncoding.EncodeToString(signature)}})
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client, err := vaultclient.New(vaultclient.Config{Addr: server.URL, MaxRetries: 1})
	require.NoError(t, err)
	now := time.Now().UTC()
	policyJWS, err := crypto.SignEdDSAJWS(crypto.DefaultMoneyMovementApprovalPolicy(tenantID, now), policyPrivate)
	require.NoError(t, err)
	provider, err := NewTransitProvider(context.Background(), TransitProviderConfig{
		Client: client, PolicyPublicKey: policyPublic, PolicyJWS: map[string]string{"default": policyJWS},
	})
	require.NoError(t, err)

	signer, err := provider.SignerForTenant(tenantID)
	require.NoError(t, err)
	intent := crypto.MoneyMovementIntent{
		SchemaVersion: crypto.MoneyMovementIntentV1,
		TenantID:      tenantID, Purpose: crypto.SigningPurposeWithdrawal,
		BasisType: "test", BasisID: id.New(), NetworkCode: "tron_nile",
		TokenID: id.New(), TokenStandard: "native",
		AmountMinorUnits:   types.NewRawAmountFromInt64(100),
		DestinationAddress: "TQZiVXDjShcTrAKbkYuoH6dVvnpcZANw7V", AddressID: id.New(),
	}
	artifacts, err := signer.BuildArtifacts(context.Background(), intent, nil, "")
	require.NoError(t, err)
	publicKeys, err := provider.PublicKeysForTenant(tenantID)
	require.NoError(t, err)
	_, err = crypto.VerifyApprovalBundle(artifacts.Bundle, publicKeys, time.Now().UTC())
	require.NoError(t, err)
}
