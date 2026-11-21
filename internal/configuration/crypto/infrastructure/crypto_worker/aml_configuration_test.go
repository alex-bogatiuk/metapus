package crypto_worker

import (
	"metapus/internal/core/id"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAMLPreflightConstructionRequiresNoApprovalKeysOrRPC(t *testing.T) {
	preflight, err := NewAMLConfigurationPreflight(CryptoProcessorConfig{TenantID: id.New().String()})
	require.NoError(t, err)
	require.NotNil(t, preflight)
	require.Zero(t, preflight.signers.Len())
	require.Nil(t, preflight.authorizations, "missing control signer is reported at readiness, never implicitly created")
}

func TestAMLPreflightReturnsConfigurationErrorsWithoutWorkerPanic(t *testing.T) {
	_, err := NewAMLConfigurationPreflight(CryptoProcessorConfig{TenantID: id.New().String(), AuthorizationSigner: "transit"})
	require.ErrorContains(t, err, "VAULT_ADDR is required")
	_, err = NewAMLConfigurationPreflight(CryptoProcessorConfig{TenantID: id.New().String(), AppEnv: "production", AuthorizationSigner: "local"})
	require.ErrorContains(t, err, "production crypto authorization signer must be transit")
	_, err = NewAMLConfigurationPreflight(CryptoProcessorConfig{AppEnv: "production", VaultToken: "synthetic"})
	require.ErrorContains(t, err, "VAULT_TOKEN is forbidden")
	_, err = NewAMLConfigurationPreflight(CryptoProcessorConfig{AppEnv: "production", SigningAuthorizationPrivateKey: "synthetic"})
	require.ErrorContains(t, err, "CRYPTO_SIGNING_AUTH_PRIVATE_KEY is forbidden")
}
