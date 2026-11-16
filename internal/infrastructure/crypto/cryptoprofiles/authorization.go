package cryptoprofiles

import (
	"errors"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/infrastructure/integrations/profiles"
	"net/http"
	"strings"

	model "metapus/internal/domain/integrations"
)

// tronHeader is TronGrid's API key header, cleared before any profile authorizes.
const tronHeader = "TRON-PRO-API-KEY"

func pathSuffix(provider model.Provider, format model.CredentialKeyFormat) func(*http.Request, string) error {
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

func pathPrefix(provider model.Provider) func(*http.Request, string) error {
	return func(r *http.Request, key string) error {
		profile, ok := rpcconfig.RPCProfileForURL(provider, r.URL.Scheme+"://"+r.URL.Host)
		tronPath := profile.Family == "tron" && (strings.HasPrefix(r.URL.Path, "/wallet/") || strings.HasPrefix(r.URL.Path, "/walletsolidity/"))
		evmPath := profile.Family == "evm" && (r.URL.Path == "" || r.URL.Path == "/")
		if !ok || !model.CredentialURLSafe.Valid(key) || (!tronPath && !evmPath) || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.User != nil || r.URL.Fragment != "" {
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

func providerLabel(provider model.Provider) string {
	d, ok := cryptointegrations.Catalog().Definition(provider)
	if ok {
		return d.Label
	}
	return "integration"
}
