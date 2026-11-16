package cryptoprofiles

import (
	"errors"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/infrastructure/integrations/profiles"
	"net/http"
	"net/url"
	"strings"

	model "metapus/internal/domain/integrations"
)

func alchemy() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.Alchemy, Authorize: authorizeAlchemy,
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

func authorizeAlchemy(r *http.Request, key string) error {
	base := *r.URL
	base.Path = "/v2/"
	profile, ok := rpcconfig.RPCProfileForURL(cryptointegrations.Alchemy, base.String())
	if !ok || r.URL.RawPath != "" || key == "" || strings.ContainsAny(key, "/\\?#%\r\n\t ") {
		return errors.New("invalid Alchemy HTTP binding or key")
	}
	suffix := strings.TrimPrefix(r.URL.Path, profile.BasePath)
	if profile.Family == "tron" {
		role, method, found := strings.Cut(strings.TrimPrefix(suffix, "/"), "/")
		if !strings.HasPrefix(suffix, "/") || !found || (role != "wallet" && role != "walletsolidity") || method == "" || strings.ContainsAny(method, "/\\.?#% \r\n\t") {
			return errors.New("invalid Alchemy TRON method path")
		}
	} else if suffix != "" && suffix != "/" {
		return errors.New("invalid Alchemy EVM method path")
	} else {
		suffix = ""
	}
	profiles.ClearAuthorization(r, tronHeader)
	r.URL.Path = profile.BasePath + "/" + key + suffix
	r.URL.RawPath = ""
	return nil
}
