package cryptoprofiles

import (
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/pkg/sdk/integrations"
	"metapus/pkg/sdk/integrations/profiles"
)

func validationCloud() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.ValidationCloud, Authorize: pathSuffix(cryptointegrations.ValidationCloud, integrations.CredentialURLSafe), Classify: gatewayHTTPFailure}
}
