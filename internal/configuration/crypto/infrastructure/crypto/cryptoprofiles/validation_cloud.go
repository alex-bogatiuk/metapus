package cryptoprofiles

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/profiles"
)

func validationCloud() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.ValidationCloud, Authorize: pathSuffix(cryptointegrations.ValidationCloud, integrations.CredentialURLSafe), Classify: gatewayHTTPFailure}
}
