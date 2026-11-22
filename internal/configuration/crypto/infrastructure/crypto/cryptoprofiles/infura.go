package cryptoprofiles

import (
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/pkg/sdk/integrations/profiles"
	"net/http"
	"time"

	"metapus/pkg/sdk/integrations"
)

func infura() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.Infura, Authorize: pathSuffix(cryptointegrations.Infura, integrations.CredentialHex32),
		Classify: func(status int, body []byte) integrations.Failure {
			if status == http.StatusPaymentRequired {
				return integrations.Throttled
			}
			return classify(status, body)
		},
		RetryAt: func(status int, headers http.Header, _ []byte, now time.Time) time.Time {
			if status == http.StatusPaymentRequired {
				return now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			}
			return profiles.RetryAt(headers.Get("Retry-After"), now)
		},
	}
}
