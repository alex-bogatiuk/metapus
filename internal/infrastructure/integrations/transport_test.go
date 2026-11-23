package integrations

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	model "github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/integrations/profiles"
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
		return model.Selection{}, ErrUnavailable
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

// fixtureProfiles stand in for a configuration's providers: fixture_suffix
// appends the key to the path, fixture_pins pins its "cursor" pagination.
var fixtureProfiles = func() *profiles.Registry {
	r, err := profiles.New(append(profiles.Platform(),
		profiles.Profile{Provider: "fixture_suffix", Authorize: func(r *http.Request, key string) error {
			r.URL.Path, r.URL.RawPath = strings.TrimRight(r.URL.Path, "/")+"/"+key, ""
			return nil
		}},
		profiles.Profile{Provider: "fixture_pins", CursorPin: &profiles.CursorPin{PathContains: "/v1/", Param: "cursor"}, Authorize: func(r *http.Request, key string) error {
			r.Header.Set("X-Fixture-Key", key)
			return nil
		}},
	)...)
	if err != nil {
		panic(err)
	}
	return r
}()

func fixture() *memoryAccounts {
	pid := id.New()
	return &memoryAccounts{selections: []model.Selection{{PoolID: pid, AccountID: id.New(), CredentialsVersion: 1, Slot: "default", Key: "key-a"}, {PoolID: pid, AccountID: id.New(), CredentialsVersion: 1, Slot: "default", Key: "key-b"}}}
}
func TestTransportRotatesPermanentKeysAndReplaysIdenticalBody(t *testing.T) {
	repo := fixture()
	var bodies, keys []string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		bodies = append(bodies, string(raw))
		keys = append(keys, r.Header.Get("x-cg-demo-api-key"))
		if len(keys) == 1 {
			return response(r, 429, `{"error":"monthly quota exceeded"}`), nil
		}
		return response(r, 200, `{"result":"ok"}`), nil
	})
	tr := &Transport{Base: base, Repository: repo, PoolID: repo.selections[0].PoolID, Provider: model.CoinGeckoDemo}
	req, err := http.NewRequest(http.MethodPost, "https://api.coingecko.com/test", strings.NewReader(`{"same":"payload"}`))
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, []string{"key-a", "key-b"}, keys)
	require.Equal(t, bodies[0], bodies[1])
	require.Equal(t, []model.Failure{model.Exhausted}, repo.reports)
}
func TestTransportPollsWithoutPersistingKeysInRequest(t *testing.T) {
	repo := fixture()
	now := time.Now().UTC()
	tr := &Transport{Profiles: fixtureProfiles, Repository: repo, PoolID: repo.selections[0].PoolID, Provider: "fixture_suffix", Now: func() time.Time { return now }, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(r, 200, `{"result":"ok"}`), nil })}
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "https://fixture.test/v2/", nil)
		resp, err := tr.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, "/v2/", req.URL.Path)
	}
	require.Equal(t, 1, repo.calls)
	now = now.Add(11 * time.Second)
	req, _ := http.NewRequest(http.MethodGet, "https://fixture.test/v2/", nil)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 2, repo.calls)
}
func TestClassificationDoesNotTreatEvery429AsMonthlyExhaustion(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   model.Failure
	}{
		{"temporary", 429, `{"error":"capacity exceeded"}`, model.Throttled},
		{"monthly", 429, `{"error":"monthly capacity limit exceeded"}`, model.Exhausted},
		{"bad key", 401, `{}`, model.Unauthorized},
		{"success", 200, `{"result":"ok"}`, ""},
		{"ordinary data", 200, `{"data":{"name":"monthly quota report"}}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, profiles.Classify(tt.status, []byte(tt.body))) })
	}
}
func TestPaginationFingerprintCannotMoveToAnotherAccount(t *testing.T) {
	repo := fixture()
	var cursor string
	tr := &Transport{Profiles: fixtureProfiles, Repository: repo, PoolID: repo.selections[0].PoolID, Provider: "fixture_pins", Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, 200, `{"data":[],"meta":{"fingerprint":"upstream-cursor"}}`), nil
	})}
	req, _ := http.NewRequest(http.MethodGet, "https://fixture.test/v1/accounts/test/transactions", nil)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, string(raw), "upstream-cursor")
	cursor, err = PinResponseCursor(resp.Header, "upstream-cursor")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(cursor, "mp1."))

	repo.current = 1
	tr.forget()
	req, _ = http.NewRequest(http.MethodGet, "https://fixture.test/v1/accounts/test/transactions?cursor="+cursor, nil)
	_, err = tr.RoundTrip(req)
	require.ErrorIs(t, err, ErrAccountChanged)
}

func TestProviderQuotaClassificationAndRetryBounds(t *testing.T) {
	coinGecko, ok := (*profiles.Registry)(nil).Lookup(model.CoinGeckoDemo)
	require.True(t, ok, "a nil registry serves the platform profiles")
	require.Equal(t, model.Unauthorized, coinGecko.Failure(200, []byte(`{"status":{"error_code": 10010}}`)))
	require.Equal(t, model.Exhausted, profiles.Classify(403, []byte(`{"error":"Monthly capacity limit exceeded"}`)))
	require.Equal(t, model.Throttled, profiles.Classify(429, []byte(`{"error":"Account includes monthly capacity; reduce requests per second"}`)))
	now := time.Now()
	require.Equal(t, now.Add(24*time.Hour), retryAt("9223372036854775807", now))
}
