package cryptoprofiles

import (
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

func validationCloud() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.ValidationCloud, Authorize: pathSuffix(cryptointegrations.ValidationCloud, integrations.CredentialURLSafe), Classify: gatewayHTTPFailure}
}
