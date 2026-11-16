package crypto_integration_repo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	model "metapus/internal/domain/integrations"
)

func TestConnectionProbeValidatesConfiguredNetworkIndependentOfVendor(t *testing.T) {
	for _, provider := range []model.Provider{model.Alchemy, model.Infura, model.Chainstack, "future-provider"} {
		for _, network := range []struct{ id, hex string }{{"1", "0x1"}, {"11155111", "0xaa36a7"}} {
			probe, err := RPCProbe(model.ConnectionTarget{Provider: provider, Family: "evm", Role: "rpc", NetworkIdentifier: network.id, URL: "https://fixture.test/"})
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
		probe, err := RPCProbe(model.ConnectionTarget{Family: "tron", Role: role, URL: "https://fixture.test/"})
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
