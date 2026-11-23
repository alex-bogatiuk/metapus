package integrations

import (
	"net/http"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/infrastructure/integrations/profiles"
	"github.com/stretchr/testify/require"
)

// A new authentication scheme participates in existing account failover without
// editing Transport, the account repository or any chain pool.
func TestNewProfileUsesSharedAccountFailover(t *testing.T) {
	repo := fixture()
	registry, err := profiles.New(profiles.Profile{Provider: "fixture-provider", Authorize: func(req *http.Request, key string) error { req.Header.Set("X-Fixture-Key", key); return nil }})
	require.NoError(t, err)
	var keys []string
	tr := &Transport{Profiles: registry, Provider: "fixture-provider", PoolID: repo.selections[0].PoolID, Repository: repo, Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		keys = append(keys, req.Header.Get("X-Fixture-Key"))
		if len(keys) == 1 {
			return response(req, 401, `{}`), nil
		}
		return response(req, 200, `{"result":"ok"}`), nil
	})}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://fixture.test/", nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, []string{"key-a", "key-b"}, keys)
	require.Empty(t, req.Header.Get("X-Fixture-Key"))
}
