// Package profiles are provider profiles: how a provider's responses are
// classified and its credentials applied.
package profiles

import (
	"net/http"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/integrations/profiles"
)

type (
	// CursorPin names a pagination cursor: the query parameter that carries it
	// and the request paths that use it. The transport hands a cursor back only
	// wrapped with the issuing account, so a page is never continued with
	// another account's key.
	CursorPin = core.CursorPin
	Probe     = core.Probe
	Profile   = core.Profile
	// Registry is immutable after construction and can be injected in tests or at
	// composition roots. No package init hooks or mutable global registrations.
	// A nil *Registry has the platform profiles only.
	Registry = core.Registry
)

var (
	// ErrAccessDenied marks a provider response that denies the operation, not the account.
	ErrAccessDenied = core.ErrAccessDenied
)

// Classify is the default classification of a failed provider response.
func Classify(status int, body []byte) integrations.Failure { return core.Classify(status, body) }

// ClassifyResponse classifies a response whose body may carry an error
// envelope the default does not know; a profile reports it as extraError.
func ClassifyResponse(status int, body []byte, extraError bool) integrations.Failure {
	return core.ClassifyResponse(status, body, extraError)
}

// ClearAuthorization removes credentials a caller or a previous attempt left
// on the request copy: the standard header, the platform's provider headers
// and any extra header a configuration's profile uses.
func ClearAuthorization(r *http.Request, extra ...string) { core.ClearAuthorization(r, extra...) }

// HeaderKey authorizes HTTPS requests to valid hosts with a key header.
func HeaderKey(header string, validHost func(string) bool, extra ...string) func(*http.Request, string) error {
	return core.HeaderKey(header, validHost, extra...)
}

// MinTime returns the earlier time.
func MinTime(a time.Time, b time.Time) time.Time { return core.MinTime(a, b) }

func New(definitions ...core.Profile) (*core.Registry, error) { return core.New(definitions...) }

// Platform lists the platform's profiles: anonymous requests and the
// exchange rate feed. A configuration appends its own and calls New.
func Platform() []core.Profile { return core.Platform() }

func RetryAt(value string, now time.Time) time.Time { return core.RetryAt(value, now) }
