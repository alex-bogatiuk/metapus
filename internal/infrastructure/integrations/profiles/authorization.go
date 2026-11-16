package profiles

import (
	"errors"
	"net/http"
	"strings"
)

// ClearAuthorization removes credentials a caller or a previous attempt left
// on the request copy: the standard header, the platform's provider headers
// and any extra header a configuration's profile uses.
func ClearAuthorization(r *http.Request, extra ...string) {
	for _, name := range append([]string{"Authorization", "X-Cg-Demo-Api-Key"}, extra...) {
		r.Header.Del(name)
	}
}

// HeaderKey authorizes HTTPS requests to valid hosts with a key header.
func HeaderKey(header string, validHost func(string) bool, extra ...string) func(*http.Request, string) error {
	return func(r *http.Request, key string) error {
		if r.URL.Scheme != "https" || !validHost(strings.ToLower(r.URL.Hostname())) || (r.URL.Port() != "" && r.URL.Port() != "443") || r.URL.User != nil || key == "" || strings.ContainsAny(key, "\r\n") {
			return errors.New("invalid integration HTTP binding or key")
		}
		ClearAuthorization(r, extra...)
		r.Header.Set(header, key)
		return nil
	}
}
