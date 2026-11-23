package cryptoprofiles

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/profiles"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/providerpolicy"
)

type chainstackRetryHint struct {
	Data struct {
		TryAgainIn string `json:"try_again_in"`
	} `json:"data"`
}

func chainstack() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.Chainstack, Authorize: pathPrefix(cryptointegrations.Chainstack),
		Classify: func(status int, body []byte) integrations.Failure {
			failure := classify(status, body)
			if status == http.StatusForbidden && failure == integrations.Unauthorized {
				return ""
			}
			return failure
		},
		RetryAt: func(_ int, headers http.Header, body []byte, now time.Time) time.Time {
			at := profiles.RetryAt(headers.Get("Retry-After"), now)
			if headers.Get("Retry-After") == "" {
				hint := readRPCError[chainstackRetryHint](body)
				if delay, err := time.ParseDuration(hint.Data.TryAgainIn); err == nil && delay > 0 {
					at = now.Add(min(delay, 24*time.Hour))
				}
			}
			return profiles.MinTime(at.Add(time.Duration(rand.IntN(251))*time.Millisecond), now.Add(24*time.Hour))
		},
		ResponseError: func(status int, body []byte) error {
			if status != http.StatusForbidden {
				return nil
			}
			denied := readRPCError[rpcErrorDetails](body)
			if denied.Code == -32002 && strings.Contains(strings.ToLower(denied.Message), "archive, debug and trace requests are not available on your current plan") {
				return providerpolicy.ErrArchiveUnavailable
			}
			return profiles.ErrAccessDenied
		},
	}
}
