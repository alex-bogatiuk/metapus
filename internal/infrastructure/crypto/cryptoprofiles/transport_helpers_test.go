package cryptoprofiles

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"metapus/internal/core/id"
	model "metapus/internal/domain/integrations"
	integrationhttp "metapus/internal/infrastructure/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type memoryAccounts struct {
	mu         sync.Mutex
	current    int
	selections []model.Selection
	reports    []model.Failure
	calls      int
}

func (r *memoryAccounts) Select(_ context.Context, _ id.ID, _ string, _ model.Provider) (model.Selection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.current >= len(r.selections) {
		return model.Selection{}, integrationhttp.ErrUnavailable
	}
	return r.selections[r.current], nil
}

func (r *memoryAccounts) Report(_ context.Context, _ model.Selection, f model.Failure, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, f)
	if f != "" {
		r.current++
	}
	return nil
}

func response(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func fixture() *memoryAccounts {
	pid := id.New()
	return &memoryAccounts{selections: []model.Selection{{PoolID: pid, AccountID: id.New(), CredentialsVersion: 1, Slot: "default", Key: "key-a"}, {PoolID: pid, AccountID: id.New(), CredentialsVersion: 1, Slot: "default", Key: "key-b"}}}
}

// classifyFor and retryHintFor ask the application registry, as the transport does.
func classifyFor(provider model.Provider, status int, body []byte) model.Failure {
	profile, ok := Registry().Lookup(provider)
	if !ok {
		return ""
	}
	return profile.Failure(status, body)
}

func retryHintFor(provider model.Provider, header string, body []byte, now time.Time) time.Time {
	profile, _ := Registry().Lookup(provider)
	headers := make(http.Header)
	headers.Set("Retry-After", header)
	return profile.NextAttempt(0, headers, body, now)
}

// fixtureProbe stands in for the endpoint probe: it posts to the binding URL,
// or to a TRON role path, and accepts any 200 body.
func fixtureProbe(target model.ConnectionTarget) (profiles.Probe, error) {
	probe := profiles.Probe{Method: http.MethodPost, URL: target.URL, Body: `{}`, Valid: func([]byte) bool { return true }}
	switch target.Role {
	case "fullnode":
		probe.URL = strings.TrimRight(target.URL, "/") + "/wallet/getnowblock"
	case "solidity":
		probe.URL = strings.TrimRight(target.URL, "/") + "/walletsolidity/getnowblock"
	}
	return probe, nil
}
