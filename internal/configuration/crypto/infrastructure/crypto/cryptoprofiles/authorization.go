package cryptoprofiles

import (
	"errors"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/profiles"
	"net/http"
	"strings"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
)

// tronHeader is TronGrid's API key header, cleared before any profile authorizes.
const tronHeader = "TRON-PRO-API-KEY"

func pathSuffix(provider integrations.Provider, format integrations.CredentialKeyFormat) func(*http.Request, string) error {
	return func(r *http.Request, key string) error {
		if _, ok := rpcconfig.RPCProfileForURL(provider, r.URL.String()); !ok || key == "" || !format.Valid(key) || strings.ContainsAny(key, "/?#%") {
			return errors.New("invalid " + providerLabel(provider) + " HTTP binding or key")
		}
		profiles.ClearAuthorization(r, tronHeader)
		r.URL.Path = strings.TrimRight(r.URL.Path, "/") + "/" + key
		r.URL.RawPath = ""
		return nil
	}
}

func pathPrefix(provider integrations.Provider) func(*http.Request, string) error {
	return func(r *http.Request, key string) error {
		profile, ok := rpcconfig.RPCProfileForURL(provider, r.URL.Scheme+"://"+r.URL.Host)
		tronPath := profile.Family == "tron" && (strings.HasPrefix(r.URL.Path, "/wallet/") || strings.HasPrefix(r.URL.Path, "/walletsolidity/"))
		evmPath := profile.Family == "evm" && (r.URL.Path == "" || r.URL.Path == "/")
		if !ok || !integrations.CredentialURLSafe.Valid(key) || (!tronPath && !evmPath) || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.User != nil || r.URL.Fragment != "" {
			return errors.New("invalid " + providerLabel(provider) + " HTTP binding or token")
		}
		profiles.ClearAuthorization(r, tronHeader)
		r.URL.Path = "/" + key + r.URL.Path
		if evmPath {
			r.URL.Path = "/" + key
		}
		r.URL.RawPath = ""
		return nil
	}
}

func providerLabel(provider integrations.Provider) string {
	d, ok := cryptointegrations.Catalog().Definition(provider)
	if ok {
		return d.Label
	}
	return "integration"
}

// chainPathKey inserts a project key after a registered EVM or native TRON base.
// Only TRON accepts a node role and a single method after the base.
func chainPathKey(provider integrations.Provider, format integrations.CredentialKeyFormat) func(*http.Request, string) error {
	return func(r *http.Request, key string) error {
		invalid := func() error { return errors.New("invalid " + providerLabel(provider) + " HTTP binding or key") }
		if r.URL.RawPath != "" || key == "" || !format.Valid(key) || strings.ContainsAny(key, "/\\?#%\r\n\t ") {
			return invalid()
		}
		base := *r.URL
		suffix := ""
		for _, prefix := range []string{"/wallet/", "/walletsolidity/"} {
			if path, method, found := strings.Cut(base.Path, prefix); found {
				if method == "" || strings.ContainsAny(method, "/\\.?#% \r\n\t") || strings.HasSuffix(path, "/") {
					return invalid()
				}
				base.Path, suffix = path, prefix+method
				break
			}
		}
		profile, ok := rpcconfig.RPCProfileForURL(provider, base.String())
		if !ok || (profile.Family == "tron") != (suffix != "") {
			return invalid()
		}
		profiles.ClearAuthorization(r, tronHeader)
		r.URL.Path = profile.BasePath + "/" + key + suffix
		return nil
	}
}
