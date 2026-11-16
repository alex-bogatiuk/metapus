package cryptoprofiles

import (
	"math/rand/v2"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/infrastructure/integrations/profiles"
	"net/http"
	"strings"
	"time"

	"metapus/internal/core/json"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/providerpolicy"
)

func chainstack() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.Chainstack, Authorize: pathPrefix(cryptointegrations.Chainstack),
		Classify: func(status int, body []byte) model.Failure {
			failure := classify(status, body)
			if status == http.StatusForbidden && failure == model.Unauthorized {
				return ""
			}
			return failure
		},
		RetryAt: func(_ int, headers http.Header, body []byte, now time.Time) time.Time {
			at := profiles.RetryAt(headers.Get("Retry-After"), now)
			var envelope struct {
				Error struct {
					Data struct {
						TryAgainIn string `json:"try_again_in"`
					} `json:"data"`
				} `json:"error"`
			}
			if headers.Get("Retry-After") == "" && json.Unmarshal(body, &envelope) == nil {
				if delay, err := time.ParseDuration(envelope.Error.Data.TryAgainIn); err == nil && delay > 0 {
					at = now.Add(min(delay, 24*time.Hour))
				}
			}
			return profiles.MinTime(at.Add(time.Duration(rand.IntN(251))*time.Millisecond), now.Add(24*time.Hour))
		},
		ResponseError: func(status int, body []byte) error {
			if status != http.StatusForbidden {
				return nil
			}
			var denied struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &denied) == nil && denied.Error.Code == -32002 && strings.Contains(strings.ToLower(denied.Error.Message), "archive, debug and trace requests are not available on your current plan") {
				return providerpolicy.ErrArchiveUnavailable
			}
			return profiles.ErrAccessDenied
		},
	}
}
