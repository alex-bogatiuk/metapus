package crypto_worker

import (
	"fmt"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto/authorization_signer"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/vaultclient"
)

// Shared by worker startup and the standalone read-only readiness service.
func configuredAuthorizationSigner(cfg CryptoProcessorConfig, client *vaultclient.Client) (crypto.SigningAuthorizationSigner, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.AuthorizationSigner))
	if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") && mode != "" && mode != "transit" {
		return nil, fmt.Errorf("production crypto authorization signer must be transit")
	}
	switch mode {
	case "":
		return nil, nil
	case "local":
		return authorization_signer.NewLocal(cfg.SigningAuthorizationPrivateKey, cfg.LocalAuthorizationKID, strings.ToLower(strings.TrimSpace(cfg.AppEnv)))
	case "transit":
		if client == nil {
			return nil, fmt.Errorf("VAULT_ADDR is required for CRYPTO_AUTH_SIGNER=transit")
		}
		return authorization_signer.NewTransit(authorization_signer.TransitConfig{Client: client, Mount: cfg.TransitMount})
	default:
		return nil, fmt.Errorf("CRYPTO_AUTH_SIGNER must be transit, local, or empty")
	}
}
