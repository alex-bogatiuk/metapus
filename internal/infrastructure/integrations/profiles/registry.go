// Package profiles owns provider-specific HTTP behavior. Account selection,
// retries and RPC routing belong to the shared callers, never to a profile.
package profiles

import (
	"errors"
	"net/http"
	"time"

	model "metapus/internal/domain/integrations"
)

type Profile struct {
	Provider          model.Provider
	Authorize         func(*http.Request, string) error
	ReplaceTargetPath bool
	PinsPagination    bool
	Classify          func(int, []byte) model.Failure
	RetryAt           func(int, http.Header, []byte, time.Time) time.Time
	ResponseError     func(int, []byte) error
	Probe             *Probe
	ImportCredentials func(string, string) (string, string, error)
}

type Probe struct {
	Method, URL, Body string
	Valid             func([]byte) bool
}

// Registry is immutable after construction and can be injected in tests or at
// composition roots. No package init hooks or mutable global registrations.
type Registry struct{ profiles map[model.Provider]Profile }

func New(definitions ...Profile) (*Registry, error) {
	r := &Registry{profiles: make(map[model.Provider]Profile, len(definitions))}
	for _, p := range definitions {
		if p.Authorize == nil {
			return nil, errors.New("integration profile requires an authorizer")
		}
		if _, exists := r.profiles[p.Provider]; exists {
			return nil, errors.New("duplicate integration profile")
		}
		r.profiles[p.Provider] = clone(p)
	}
	return r, nil
}

func (r *Registry) Lookup(provider model.Provider) (Profile, bool) {
	if r == nil {
		r = builtins
	}
	p, ok := r.profiles[provider]
	return clone(p), ok
}

func clone(p Profile) Profile {
	if p.Probe != nil {
		copy := *p.Probe
		p.Probe = &copy
	}
	return p
}

func (p Profile) Failure(status int, body []byte) model.Failure {
	if p.Classify != nil {
		return p.Classify(status, body)
	}
	return classify(status, body)
}

func (p Profile) NextAttempt(status int, headers http.Header, body []byte, now time.Time) time.Time {
	if p.RetryAt != nil {
		return p.RetryAt(status, headers, body, now)
	}
	return RetryAt(headers.Get("Retry-After"), now)
}

func (p Profile) MinRequestIntervalMS() int {
	d, _ := p.Provider.Definition()
	return d.AccountMinRequestIntervalMS
}

var builtins = newBuiltins()

func newBuiltins() *Registry {
	r, err := New(
		Profile{Provider: "", Authorize: func(*http.Request, string) error { return nil }},
		alchemy(), infura(), drpc(), tronAPI(), chainstack(), getBlock(), coinGecko(),
	)
	if err != nil {
		panic(err)
	}
	return r
}
