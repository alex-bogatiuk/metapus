package profiles

import (
	"errors"
	"net/http"
	"strings"

	model "metapus/internal/domain/integrations"
)

func clearAuthorization(r *http.Request) {
	for _, name := range []string{"Authorization", "TRON-PRO-API-KEY", "X-Cg-Demo-Api-Key"} {
		r.Header.Del(name)
	}
}

func pathSuffix(provider model.Provider, format model.CredentialKeyFormat) func(*http.Request, string) error {
	return func(r *http.Request, key string) error {
		if _, ok := provider.RPCProfileForURL(r.URL.String()); !ok || key == "" || !format.Valid(key) || strings.ContainsAny(key, "/?#%") {
			return errors.New("invalid " + providerLabel(provider) + " HTTP binding or key")
		}
		clearAuthorization(r)
		r.URL.Path = strings.TrimRight(r.URL.Path, "/") + "/" + key
		r.URL.RawPath = ""
		return nil
	}
}

func pathPrefix(provider model.Provider) func(*http.Request, string) error {
	return func(r *http.Request, key string) error {
		profile, ok := provider.RPCProfileForURL(r.URL.Scheme + "://" + r.URL.Host)
		tronPath := profile.Family == "tron" && (strings.HasPrefix(r.URL.Path, "/wallet/") || strings.HasPrefix(r.URL.Path, "/walletsolidity/"))
		evmPath := profile.Family == "evm" && (r.URL.Path == "" || r.URL.Path == "/")
		if !ok || !model.CredentialURLSafe.Valid(key) || (!tronPath && !evmPath) || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.User != nil || r.URL.Fragment != "" {
			return errors.New("invalid " + providerLabel(provider) + " HTTP binding or token")
		}
		clearAuthorization(r)
		r.URL.Path = "/" + key + r.URL.Path
		if evmPath {
			r.URL.Path = "/" + key
		}
		r.URL.RawPath = ""
		return nil
	}
}

func headerKey(header string, validHost func(string) bool) func(*http.Request, string) error {
	return func(r *http.Request, key string) error {
		if r.URL.Scheme != "https" || !validHost(strings.ToLower(r.URL.Hostname())) || (r.URL.Port() != "" && r.URL.Port() != "443") || r.URL.User != nil || key == "" || strings.ContainsAny(key, "\r\n") {
			return errors.New("invalid integration HTTP binding or key")
		}
		clearAuthorization(r)
		r.Header.Set(header, key)
		return nil
	}
}

func providerLabel(provider model.Provider) string {
	d, ok := provider.Definition()
	if ok {
		return d.Label
	}
	return "integration"
}
