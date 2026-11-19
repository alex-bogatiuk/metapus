package cryptoprofiles

import (
	"errors"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/infrastructure/integrations/profiles"
	"net/http"
	"net/url"
	"strings"

	model "metapus/internal/domain/integrations"
)

func alchemy() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.Alchemy, Authorize: chainPathKey(cryptointegrations.Alchemy, ""),
		Classify: func(status int, body []byte) model.Failure {
			if alchemyNetworkDisabled(status, body) {
				return ""
			}
			return classify(status, body)
		},
		ResponseError: func(status int, body []byte) error {
			if alchemyNetworkDisabled(status, body) {
				return profiles.ErrAccessDenied
			}
			return nil
		},
		ImportCredentials: func(raw, key string) (string, string, error) {
			u, err := url.Parse(raw)
			// chainconfig.Load trims the trailing slash, so a base with a
			// separate key file arrives as /v2.
			if err == nil && u.Path == "/v2" {
				u.Path = "/v2/"
			}
			if err != nil || !strings.HasPrefix(u.Path, "/v2/") {
				return "", "", errors.New("unsupported credential endpoint")
			}
			fromURL := strings.TrimPrefix(u.Path, "/v2/")
			if key != "" && fromURL != "" && fromURL != key {
				return "", "", errors.New("conflicting integration credentials")
			}
			if fromURL != "" {
				key = fromURL
			}
			if key == "" || strings.ContainsAny(key, "/?#%") {
				return "", "", errors.New("invalid integration credential")
			}
			u.Path, u.RawPath = "/v2/", ""
			return u.String(), key, nil
		},
	}
}

func alchemyNetworkDisabled(status int, body []byte) bool {
	return status == http.StatusForbidden && strings.Contains(strings.ToLower(string(body)), "is not enabled for this app")
}
