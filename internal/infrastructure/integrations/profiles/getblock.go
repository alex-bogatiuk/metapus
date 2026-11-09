package profiles

import (
	"net/http"
	"strings"

	model "metapus/internal/domain/integrations"
)

func getBlock() Profile {
	return Profile{Provider: model.GetBlock, Authorize: pathPrefix(model.GetBlock),
		Classify: func(status int, body []byte) model.Failure {
			if status == http.StatusPaymentRequired || strings.Contains(strings.ToLower(string(body)), "out of cu") {
				return model.Exhausted
			}
			return classify(status, body)
		},
	}
}
