package cryptoprofiles

import (
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/pkg/sdk/integrations/profiles"
	"net/http"
	"strings"

	"metapus/pkg/sdk/integrations"
)

func getBlock() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.GetBlock, Authorize: pathPrefix(cryptointegrations.GetBlock),
		Classify: func(status int, body []byte) integrations.Failure {
			if status == http.StatusPaymentRequired || strings.Contains(strings.ToLower(string(body)), "out of cu") {
				return integrations.Exhausted
			}
			return classify(status, body)
		},
	}
}
