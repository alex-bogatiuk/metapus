package integrations

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	model "metapus/internal/domain/integrations"
)

func TestConnectionProbeValidatesConfiguredNetworkIndependentOfVendor(t *testing.T) {
	for _, provider := range []model.Provider{model.Alchemy, model.Infura, model.Chainstack, "future-provider"} {
		for _, network := range []struct{ id, hex string }{{"1", "0x1"}, {"11155111", "0xaa36a7"}} {
			probe, err := rpcProbe(model.ConnectionTarget{Provider: provider, Family: "evm", Role: "rpc", NetworkIdentifier: network.id, URL: "https://fixture.test/"})
			require.NoError(t, err)
			require.True(t, probe.Valid([]byte(`{"jsonrpc":"2.0","result":"`+network.hex+`"}`)))
			require.False(t, probe.Valid([]byte(`{"jsonrpc":"2.0","result":"0x2"}`)))
			require.False(t, probe.Valid([]byte(`{"error":{"code":402},"result":"`+network.hex+`"}`)))
			require.False(t, probe.Valid([]byte(`{"result":"0x"}`)))
		}
	}
}

func TestConnectionProbeSelectsTRONRoleAndRejectsIncompleteEvidence(t *testing.T) {
	for _, role := range []string{"fullnode", "solidity"} {
		probe, err := rpcProbe(model.ConnectionTarget{Family: "tron", Role: role, URL: "https://fixture.test/"})
		require.NoError(t, err)
		path := "/wallet/getnowblock"
		if role == "solidity" {
			path = "/walletsolidity/getnowblock"
		}
		require.Equal(t, "https://fixture.test"+path, probe.URL)
		require.True(t, probe.Valid([]byte(`{"blockID":"`+strings.Repeat("a", 64)+`","block_header":{"raw_data":{"number":100}}}`)))
		require.False(t, probe.Valid([]byte(`{}`)))
		require.False(t, probe.Valid([]byte(`{"blockID":"`+strings.Repeat("a", 64)+`","Error":"access denied"}`)))
	}
}

func TestConnectionProberUsesActualBindingAndRejectsRedirects(t *testing.T) {
	for _, status := range []int{200, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			target := model.ConnectionTarget{PoolID: id.New(), EndpointID: id.New(), Provider: model.Alchemy, URL: "https://eth-mainnet.g.alchemy.com/v2/", Family: "evm", Role: "rpc", NetworkIdentifier: "1"}
			calls := 0
			prober := ConnectionProber{EndpointTransport: func(uid id.ID, base string) func(http.RoundTripper) http.RoundTripper {
				require.Equal(t, target.EndpointID, uid)
				require.Equal(t, target.URL, base)
				return func(http.RoundTripper) http.RoundTripper {
					return roundTripFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						require.Equal(t, target.URL, req.URL.String())
						resp := response(req, status, `{"result":"0x1"}`)
						resp.Header.Set("Location", "https://foreign.test/private-token")
						return resp, nil
					})
				}
			}}
			ready, err := prober.Probe(t.Context(), target)
			require.NoError(t, err)
			require.Equal(t, status == 200, ready)
			require.Equal(t, 1, calls)
		})
	}
}
