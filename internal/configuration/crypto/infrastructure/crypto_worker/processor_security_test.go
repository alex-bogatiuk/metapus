package crypto_worker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCryptoProcessorRejectsStaticVaultTokenInProduction(t *testing.T) {
	require.PanicsWithValue(t, "VAULT_TOKEN is forbidden in production; use tokenless Vault Proxy mode", func() {
		NewCryptoProcessor(CryptoProcessorConfig{AppEnv: "production", VaultToken: "static-token"}, nil)
	})
}

func TestCryptoProcessorRejectsLocalAuthorizationKeyInProduction(t *testing.T) {
	require.PanicsWithValue(t, "CRYPTO_SIGNING_AUTH_PRIVATE_KEY is forbidden in production; use Vault Transit", func() {
		NewCryptoProcessor(CryptoProcessorConfig{AppEnv: "production", SigningAuthorizationPrivateKey: "secret"}, nil)
	})
}
