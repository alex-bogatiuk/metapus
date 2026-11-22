package crypto_integration_repo

import (
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/id"

	"metapus/pkg/sdk/integrations"
)

func TestConnectionProbeValidatesConfiguredNetworkIndependentOfVendor(t *testing.T) {
	for _, provider := range []integrations.Provider{cryptointegrations.Alchemy, cryptointegrations.Infura, cryptointegrations.Chainstack, cryptointegrations.DRPC} {
		for _, network := range []struct{ id, hex string }{{"1", "0x1"}, {"11155111", "0xaa36a7"}} {
			probe, err := RPCProbe(integrations.ConnectionTarget{Provider: provider, EndpointID: id.New(), Attributes: RPCTargetAttributes("evm", network.id, "rpc"), URL: "https://fixture.test/"})
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
		probe, err := RPCProbe(integrations.ConnectionTarget{Provider: cryptointegrations.TronAPI, EndpointID: id.New(), Attributes: RPCTargetAttributes("tron", "", role), URL: "https://fixture.test/"})
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

func TestRPCOperationNamesTRONMethods(t *testing.T) {
	for path, want := range map[string]string{
		"/v2/key/wallet/getnowblock":         "getnowblock",
		"/walletsolidity/gettransactionbyid": "gettransactionbyid",
		"/v2/key":                            "",
	} {
		require.Equal(t, want, RPCOperation(httptest.NewRequest(http.MethodPost, "https://fixture.test"+path, nil)), path)
	}
}

func TestRPCProbeRejectsOtherKindsAndPoolsWithoutEndpoint(t *testing.T) {
	_, err := RPCProbe(integrations.ConnectionTarget{Provider: integrations.CoinGeckoDemo, EndpointID: id.New(), Attributes: RPCTargetAttributes("evm", "1", "rpc"), URL: "https://fixture.test/"})
	require.ErrorContains(t, err, "подсистемой")
	_, err = RPCProbe(integrations.ConnectionTarget{Provider: cryptointegrations.Alchemy, Attributes: RPCTargetAttributes("evm", "1", "rpc"), URL: "https://fixture.test/"})
	require.ErrorContains(t, err, "RPC-подключение")
}
