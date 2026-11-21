package content

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

// setValidServerSettings sets the minimum environment loadServerSettings
// accepts.
func setValidServerSettings(t *testing.T) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	for key, value := range map[string]string{
		"APP_ENV":                           "development",
		"PUBLIC_CHECKOUT_BASE_URL":          "https://pay.example.test",
		"VAULT_ADDR":                        "http://vault:8200",
		"CRYPTO_APPROVAL_POLICY_PUBLIC_KEY": base64.StdEncoding.EncodeToString(pub),
		"CRYPTO_APPROVAL_POLICY_BUNDLE_JWS": "header.payload.signature",
	} {
		t.Setenv(key, value)
	}
}

func TestLoadServerSettingsAppliesDefaults(t *testing.T) {
	setValidServerSettings(t)

	settings, err := loadServerSettings()

	require.NoError(t, err)
	require.Equal(t, "transit", settings.CryptoTransitMount)
	require.Equal(t, "secret", settings.WebhookSecretKVMount)
	require.Equal(t, map[string]string{"default": "header.payload.signature"}, settings.ApprovalPolicyJWS)
	require.Len(t, settings.ApprovalPolicyKey, ed25519.PublicKeySize)
}

func TestLoadServerSettingsParsesPolicyBundlesByTenant(t *testing.T) {
	setValidServerSettings(t)
	t.Setenv("CRYPTO_APPROVAL_POLICY_BUNDLE_JWS", "")
	t.Setenv("CRYPTO_APPROVAL_POLICY_BUNDLES_JWS", `{"acme":"a.b.c"}`)

	settings, err := loadServerSettings()

	require.NoError(t, err)
	require.Equal(t, map[string]string{"acme": "a.b.c"}, settings.ApprovalPolicyJWS)
}

func TestLoadServerSettingsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		error string
	}{
		{name: "missing checkout URL", env: map[string]string{"PUBLIC_CHECKOUT_BASE_URL": ""}, error: "PUBLIC_CHECKOUT_BASE_URL"},
		{name: "missing Vault", env: map[string]string{"VAULT_ADDR": ""}, error: "VAULT_ADDR"},
		{name: "invalid policy key", env: map[string]string{"CRYPTO_APPROVAL_POLICY_PUBLIC_KEY": "bad"}, error: "CRYPTO_APPROVAL_POLICY_PUBLIC_KEY"},
		{name: "both policy forms", env: map[string]string{"CRYPTO_APPROVAL_POLICY_BUNDLES_JWS": `{"acme":"a.b.c"}`}, error: "configure only one"},
		{name: "empty policy map", env: map[string]string{"CRYPTO_APPROVAL_POLICY_BUNDLE_JWS": "", "CRYPTO_APPROVAL_POLICY_BUNDLES_JWS": `{}`}, error: "at least one signed policy"},
		{name: "vault token in production", env: map[string]string{"APP_ENV": "production", "VAULT_TOKEN": "root"}, error: "VAULT_TOKEN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidServerSettings(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			_, err := loadServerSettings()

			require.ErrorContains(t, err, tt.error)
		})
	}
}
