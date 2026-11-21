package cryptoprofiles

import (
	"context"
	"io"
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	integrationhttp "metapus/internal/infrastructure/integrations"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	model "metapus/internal/domain/integrations"
)

func TestAlchemyRotatesKeysPreservingProtocolPathAndBody(t *testing.T) {
	for _, tt := range []struct {
		name, host, path, body string
	}{
		{"evm", "eth-sepolia.g.alchemy.com", "", `{"jsonrpc":"2.0","id":7,"method":"eth_chainId","params":[]}`},
		{"fullnode", "tron-testnet.g.alchemy.com", "/wallet/getblockbynum", `{"num":10}`},
		{"solidity", "tron-testnet.g.alchemy.com", "/walletsolidity/gettransactioninfobyblocknum", `{"num":10}`},
		{"mainnet", "tron-mainnet.g.alchemy.com", "/walletsolidity/getnowblock", `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := fixture()
			base := "https://" + tt.host + "/v2/"
			var paths []string
			tr := &integrationhttp.Transport{Profiles: Registry(), Repository: repo, OriginalURL: base, Target: func(context.Context) (integrationhttp.Binding, error) {
				return integrationhttp.Binding{URL: strings.TrimRight(base, "/"), PoolID: repo.selections[0].PoolID, Provider: cryptointegrations.Alchemy}, nil
			}, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, tt.body, string(body))
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("TRON-PRO-API-KEY"))
				paths = append(paths, r.URL.Path)
				if len(paths) == 1 {
					return response(r, 429, `{"error":"Monthly capacity limit exceeded"}`), nil
				}
				return response(r, 200, `{}`), nil
			})}
			t.Cleanup(tr.CloseIdleConnections)
			original := strings.TrimRight(base, "/") + tt.path
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, original, strings.NewReader(tt.body))
			require.NoError(t, err)
			req.Header.Set("Authorization", "unused")
			req.Header.Set("TRON-PRO-API-KEY", "unused")
			resp, err := tr.RoundTrip(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			require.Equal(t, []string{"/v2/key-a" + tt.path, "/v2/key-b" + tt.path}, paths)
			require.Equal(t, original, req.URL.String())
			require.Equal(t, "unused", req.Header.Get("Authorization"))
			require.Equal(t, []model.Failure{model.Exhausted}, repo.reports)
		})
	}
}

func TestAlchemyRejectsUnsafeAndCrossProtocolPathsBeforeSendingKey(t *testing.T) {
	for _, give := range []string{
		"https://foreign.test/v2/",
		"https://tron-testnet.g.alchemy.com/v2/",
		"https://eth-sepolia.g.alchemy.com/v2/wallet/getnowblock",
		"https://tron-testnet.g.alchemy.com/v2/v1/accounts/address",
		"https://tron-testnet.g.alchemy.com/v2/wallet/",
		"https://tron-testnet.g.alchemy.com/v2/wallet/../getnowblock",
		"https://tron-testnet.g.alchemy.com/v2/wallet/%67etnowblock",
		"https://tron-testnet.g.alchemy.com/v2/wallet/getnowblock?key=x",
		"https://tron-testnet.g.alchemy.com/v2/wallet/getnowblock?",
		"https://tron-testnet.g.alchemy.com/v2/wallet/getnowblock#fragment",
	} {
		t.Run(give, func(t *testing.T) {
			repo := fixture()
			tr := &integrationhttp.Transport{Profiles: Registry(), PoolID: repo.selections[0].PoolID, Provider: cryptointegrations.Alchemy, Repository: repo, Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid request reached upstream")
				return nil, nil
			})}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, give, nil)
			require.NoError(t, err)
			_, err = tr.RoundTrip(req)
			require.ErrorContains(t, err, "invalid Alchemy")
			require.NotContains(t, err.Error(), repo.selections[0].Key)
		})
	}
}

func TestAlchemyTRONConnectionProbeUsesAuthenticatedRolePath(t *testing.T) {
	for _, role := range []string{"fullnode", "solidity"} {
		t.Run(role, func(t *testing.T) {
			repo := fixture()
			target := model.ConnectionTarget{PoolID: repo.selections[0].PoolID, EndpointID: id.New(), Provider: cryptointegrations.Alchemy, URL: "https://tron-testnet.g.alchemy.com/v2/", Attributes: map[string]string{"family": "tron", "network": "nile", "role": role}}
			prober := integrationhttp.ConnectionProber{Profiles: Registry(), EndpointProbe: fixtureProbe, EndpointTransport: func(id.ID, string) func(http.RoundTripper) http.RoundTripper {
				return func(http.RoundTripper) http.RoundTripper {
					return &integrationhttp.Transport{Profiles: Registry(), PoolID: target.PoolID, Provider: target.Provider, Repository: repo, OriginalURL: target.URL, Target: func(context.Context) (integrationhttp.Binding, error) {
						return integrationhttp.Binding{URL: target.URL, PoolID: target.PoolID, Provider: target.Provider}, nil
					}, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						prefix := "wallet"
						if role == "solidity" {
							prefix = "walletsolidity"
						}
						require.Equal(t, "/v2/key-a/"+prefix+"/getnowblock", r.URL.Path)
						return response(r, 200, `{"blockID":"`+strings.Repeat("a", 64)+`","block_header":{"raw_data":{"number":100}}}`), nil
					})}
				}
			}}
			ready, err := prober.Probe(t.Context(), target)
			require.NoError(t, err)
			require.True(t, ready)
		})
	}
}

func TestAlchemyDisabledNetworkDoesNotInvalidateSharedAccount(t *testing.T) {
	repo := fixture()
	calls := 0
	tr := &integrationhttp.Transport{Profiles: Registry(), PoolID: repo.selections[0].PoolID, Provider: cryptointegrations.Alchemy, Repository: repo, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return response(r, 403, "TRON_TESTNET is not enabled for this app. Visit https://dashboard.alchemy.com/apps/private-app/networks"), nil
	})}
	t.Cleanup(tr.CloseIdleConnections)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://tron-testnet.g.alchemy.com/v2/wallet/getnowblock", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	require.ErrorIs(t, err, integrationhttp.ErrAccessDenied)
	require.NotContains(t, err.Error(), "private-app")
	require.Equal(t, 1, calls)
	require.Empty(t, repo.reports)
}
