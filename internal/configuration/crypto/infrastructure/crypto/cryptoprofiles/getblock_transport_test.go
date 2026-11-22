package cryptoprofiles

import (
	"io"
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/pkg/sdk/integrations/transport"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/pkg/sdk/integrations"
)

func TestGetBlockRotatesAccountsWithoutChangingRoleOrBody(t *testing.T) {
	for _, role := range []string{"wallet", "walletsolidity"} {
		t.Run(role, func(t *testing.T) {
			repo := fixture()
			var paths []string
			tr := &transport.Transport{Profiles: Registry(), PoolID: repo.selections[0].PoolID, Provider: cryptointegrations.GetBlock, Repository: repo, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.Path)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, `{"num":71963230}`, string(body))
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("TRON-PRO-API-KEY"))
				if len(paths) == 1 {
					return response(r, 401, `{"error":"access denied"}`), nil
				}
				return response(r, 200, `[]`), nil
			})}
			t.Cleanup(tr.CloseIdleConnections)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://shared.eu-central-1.getblock.io/"+role+"/gettransactioninfobyblocknum", strings.NewReader(`{"num":71963230}`))
			require.NoError(t, err)
			req.Header.Set("Authorization", "old-value")
			req.Header.Set("TRON-PRO-API-KEY", "old-value")
			resp, err := tr.RoundTrip(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			require.Equal(t, []string{"/key-a/" + role + "/gettransactioninfobyblocknum", "/key-b/" + role + "/gettransactioninfobyblocknum"}, paths)
			require.Equal(t, "/"+role+"/gettransactioninfobyblocknum", req.URL.Path)
			require.Equal(t, []integrations.Failure{integrations.Unauthorized}, repo.reports)
		})
	}
}

func TestGetBlockRejectsUnsafeBindingsBeforeSendingCredentials(t *testing.T) {
	for _, target := range []string{
		"https://example.com/wallet/getnowblock",
		"http://shared.eu-central-1.getblock.io/wallet/getnowblock",
		"https://shared.eu-central-1.getblock.io:8443/wallet/getnowblock",
		"https://shared.eu-central-1.getblock.io/token/wallet/getnowblock",
		"https://shared.eu-central-1.getblock.io/wallet/getnowblock?key=secret",
		"https://shared.eu-central-1.getblock.io/v1/accounts",
		"https://user@shared.eu-central-1.getblock.io/wallet/getnowblock",
	} {
		t.Run(target, func(t *testing.T) {
			repo := fixture()
			tr := &transport.Transport{Profiles: Registry(), PoolID: repo.selections[0].PoolID, Provider: cryptointegrations.GetBlock, Repository: repo, Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid binding must never send a credential")
				return nil, nil
			})}
			t.Cleanup(tr.CloseIdleConnections)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, target, nil)
			require.NoError(t, err)
			_, err = tr.RoundTrip(req)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "key-a")
			require.NotContains(t, err.Error(), target)
		})
	}
	for _, token := range []string{"https://example.com/secret", "a/b", "a%2fb", "a?b", "a#b", "a b"} {
		repo := fixture()
		repo.selections[0].Key = token
		tr := &transport.Transport{Profiles: Registry(), PoolID: repo.selections[0].PoolID, Provider: cryptointegrations.GetBlock, Repository: repo}
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://shared.eu-central-1.getblock.io/wallet/getnowblock", nil)
		_, err := tr.RoundTrip(req)
		require.Error(t, err)
		require.NotContains(t, err.Error(), token)
	}
	require.Equal(t, integrations.Throttled, classifyFor(cryptointegrations.GetBlock, 429, []byte(`{"error":"Too many requests"}`)))
	require.Equal(t, integrations.Exhausted, classifyFor(cryptointegrations.GetBlock, 429, []byte(`{"error":"monthly CU quota exceeded"}`)))
	require.Equal(t, integrations.Exhausted, classifyFor(cryptointegrations.GetBlock, 402, []byte("Out of CU")))
	require.Equal(t, integrations.Exhausted, classifyFor(cryptointegrations.GetBlock, 402, []byte("")))
	require.Equal(t, integrations.Unauthorized, classifyFor(cryptointegrations.GetBlock, 403, []byte(`{"error":"access denied"}`)))
}
