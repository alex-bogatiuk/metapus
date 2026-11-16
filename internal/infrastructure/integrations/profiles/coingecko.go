package profiles

import (
	"metapus/internal/core/json"
	model "metapus/internal/domain/integrations"
)

func coinGecko() Profile {
	return Profile{Provider: model.CoinGeckoDemo, Authorize: HeaderKey("x-cg-demo-api-key", func(host string) bool { return host == "api.coingecko.com" }),
		Probe: &Probe{Method: "GET", URL: "https://api.coingecko.com/api/v3/ping", Valid: func(body []byte) bool {
			var v struct {
				Says string `json:"gecko_says"`
			}
			return json.Unmarshal(body, &v) == nil && v.Says != ""
		}},
		Classify: func(status int, body []byte) model.Failure {
			failure := Classify(status, body)
			if failure != "" {
				return failure
			}
			var envelope struct {
				Status struct {
					ErrorCode int `json:"error_code"`
				} `json:"status"`
			}
			_ = json.Unmarshal(body, &envelope)
			if envelope.Status.ErrorCode == 10002 || envelope.Status.ErrorCode == 10010 || envelope.Status.ErrorCode == 10011 {
				return model.Unauthorized
			}
			return ""
		},
	}
}
