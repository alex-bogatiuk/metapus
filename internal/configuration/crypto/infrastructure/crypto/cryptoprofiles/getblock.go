package cryptoprofiles

import (
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/internal/infrastructure/integrations/profiles"
	"net/http"
	"strings"

	model "metapus/internal/domain/integrations"
)

func getBlock() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.GetBlock, Authorize: pathPrefix(cryptointegrations.GetBlock),
		Classify: func(status int, body []byte) model.Failure {
			if status == http.StatusPaymentRequired || strings.Contains(strings.ToLower(string(body)), "out of cu") {
				return model.Exhausted
			}
			return classify(status, body)
		},
	}
}
