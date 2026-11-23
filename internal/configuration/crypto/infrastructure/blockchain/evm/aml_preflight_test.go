package evm

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/token"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestAMLEmptyCheckpointPinsEVMStateToFinalBlock(t *testing.T) {
	for _, scenario := range []string{"empty", "erc20", "nonempty", "one_source", "same_group", "different_balance", "reorg"} {
		t.Run(scenario, func(t *testing.T) {
			server := func(index int) *httptest.Server {
				var heads atomic.Int32
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						ID     json.RawMessage   `json:"id"`
						Method string            `json:"method"`
						Params []json.RawMessage `json:"params"`
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
					var result any
					switch req.Method {
					case "eth_chainId":
						result = "0xaa36a7"
					case "eth_getBlockByNumber":
						header := &gethtypes.Header{Number: big.NewInt(10), Difficulty: big.NewInt(0), GasLimit: 30000000, Time: 1700000000, BaseFee: big.NewInt(1)}
						if scenario == "reorg" && index == 1 && heads.Add(1) > 2 {
							header.Root = common.HexToHash("0xff")
						}
						result = header
					case "eth_getBalance", "eth_call":
						require.Len(t, req.Params, 2)
						require.JSONEq(t, `"0xa"`, string(req.Params[1]), "balance must use the final checkpoint, never latest")
						result = "0x0"
						if scenario == "erc20" {
							result = "0x0000000000000000000000000000000000000000000000000000000000000000"
						}
						if scenario == "nonempty" || scenario == "different_balance" && index == 1 {
							result = "0x5"
						}
					default:
						t.Errorf("unexpected checkpoint RPC %s", req.Method)
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				}))
			}
			first, second := server(0), server(1)
			defer first.Close()
			defer second.Close()
			endpoints := []chainconfig.Endpoint{{ProviderID: "one", TrustGroup: "one", URL: first.URL}, {ProviderID: "two", TrustGroup: "two", URL: second.URL}}
			if scenario == "one_source" {
				endpoints = endpoints[:1]
			}
			if scenario == "same_group" {
				endpoints[1].TrustGroup = "one"
			}
			pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "TEST", ExpectedChainID: big.NewInt(11155111), Endpoints: endpoints, FailureThreshold: 2, OpenDuration: time.Second})
			require.NoError(t, err)
			defer pool.Close()
			driver := &Driver{network: crypto.NetworkRef{ID: id.New()}, rpc: pool}
			asset := crypto.AssetRef{TokenID: id.New(), Standard: token.TokenStandardNative}
			if scenario == "erc20" {
				asset.Standard = token.TokenStandardERC20
				asset.ContractAddress = "0x0000000000000000000000000000000000000002"
			}
			proof, err := driver.EmptyCheckpoint(t.Context(), asset, "0x0000000000000000000000000000000000000001")
			if scenario == "empty" || scenario == "erc20" || scenario == "nonempty" {
				require.NoError(t, err)
				require.Equal(t, scenario != "nonempty", proof.Empty())
				require.Equal(t, int64(10), proof.Height)
				require.Len(t, proof.Sources, 2)
			} else {
				require.Error(t, err)
			}
		})
	}
}
