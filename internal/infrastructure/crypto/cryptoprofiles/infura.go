package cryptoprofiles

import (
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/infrastructure/integrations/profiles"
	"net/http"
	"time"

	model "metapus/internal/domain/integrations"
)

func infura() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.Infura, Authorize: pathSuffix(cryptointegrations.Infura, model.CredentialHex32),
		Classify: func(status int, body []byte) model.Failure {
			if status == http.StatusPaymentRequired {
				return model.Throttled
			}
			return profiles.Classify(status, body)
		},
		RetryAt: func(status int, headers http.Header, _ []byte, now time.Time) time.Time {
			if status == http.StatusPaymentRequired {
				return now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			}
			return profiles.RetryAt(headers.Get("Retry-After"), now)
		},
	}
}
