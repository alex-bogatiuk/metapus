package cryptoprofiles

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/transport"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/stretchr/testify/require"
)

type infuraAccounts struct {
	*memoryAccounts
	retry time.Time
}

func (a *infuraAccounts) Report(ctx context.Context, s integrations.Selection, f integrations.Failure, retry time.Time) error {
	a.retry = retry
	return a.memoryAccounts.Report(ctx, s, f, retry)
}

func TestInfuraAccountFailoverPreservesRPCAndDoesNotExposeKeys(t *testing.T) {
	for _, status := range []int{401, 429, 402} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			repo := &infuraAccounts{memoryAccounts: fixture()}
			repo.selections[0].Key = strings.Repeat("a", 32)
			repo.selections[1].Key = strings.Repeat("b", 32)
			now := time.Date(2026, 10, 22, 23, 59, 55, 0, time.UTC)
			var paths []string
			payload := `{"jsonrpc":"2.0","id":37,"method":"eth_getBlockByNumber","params":["0xb46013",true]}`
			tr := &transport.Transport{Profiles: Registry(), PoolID: repo.selections[0].PoolID, Provider: cryptointegrations.Infura, Slot: "sepolia", Repository: repo, Now: func() time.Time { return now }, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.Path)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, payload, string(body))
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("TRON-PRO-API-KEY"))
				if len(paths) == 1 {
					return response(r, status, `{"error":{"message":"limit or access denied"}}`), nil
				}
				return response(r, 200, `{"jsonrpc":"2.0","id":37,"result":{}}`), nil
			})}
			t.Cleanup(tr.CloseIdleConnections)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://sepolia.infura.io/v3/", strings.NewReader(payload))
			require.NoError(t, err)
			req.Header.Set("Authorization", "old-value")
			resp, err := tr.RoundTrip(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			require.Equal(t, []string{"/v3/" + strings.Repeat("a", 32), "/v3/" + strings.Repeat("b", 32)}, paths)
			require.Equal(t, "/v3/", req.URL.Path)
			if status == 402 {
				require.Equal(t, []integrations.Failure{integrations.Throttled}, repo.reports)
				require.Equal(t, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), repo.retry)
			}
		})
	}
}

func TestInfuraRejectsWrongHostsCredentialURLsAndUnsafeKeys(t *testing.T) {
	for _, target := range []string{"https://evil.test/v3/", "https://sepolia.infura.io.evil.test/v3/", "https://mainnet.infura.io/v3/", "https://sepolia.infura.io/v3/key", "https://sepolia.infura.io/v3/?", "http://sepolia.infura.io/v3/"} {
		repo := fixture()
		repo.selections[0].Key = strings.Repeat("a", 32)
		tr := &transport.Transport{Profiles: Registry(), PoolID: repo.selections[0].PoolID, Provider: cryptointegrations.Infura, Repository: repo, Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("unsafe target must not receive credentials")
			return nil, nil
		})}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, target, nil)
		require.NoError(t, err)
		_, err = tr.RoundTrip(req)
		require.Error(t, err)
		require.NotContains(t, err.Error(), target)
		require.NotContains(t, err.Error(), repo.selections[0].Key)
	}
	require.Equal(t, integrations.Throttled, classifyFor(cryptointegrations.Infura, 402, []byte(`{"error":{"code":402,"event":-33000}}`)))
	require.Equal(t, integrations.Throttled, classifyFor(cryptointegrations.Infura, 429, nil))
}
