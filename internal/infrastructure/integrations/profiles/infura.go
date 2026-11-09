package profiles

import (
	"net/http"
	"time"

	model "metapus/internal/domain/integrations"
)

func infura() Profile {
	return Profile{Provider: model.Infura, Authorize: pathSuffix(model.Infura, model.CredentialHex32),
		Classify: func(status int, body []byte) model.Failure {
			if status == http.StatusPaymentRequired {
				return model.Throttled
			}
			return classify(status, body)
		},
		RetryAt: func(status int, headers http.Header, _ []byte, now time.Time) time.Time {
			if status == http.StatusPaymentRequired {
				return now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			}
			return RetryAt(headers.Get("Retry-After"), now)
		},
	}
}
