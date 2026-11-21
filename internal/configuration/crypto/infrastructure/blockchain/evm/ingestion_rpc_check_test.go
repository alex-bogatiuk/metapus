package evm

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/json"
)

func TestRPCCheckExposesBrokenEndpointInsteadOfHidingItWithFallback(t *testing.T) {
	key, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	to := common.HexToAddress("0x1234")
	transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(1), To: &to, Value: big.NewInt(1)}), types.LatestSignerForChainID(big.NewInt(11155111)), key)
	require.NoError(t, err)
	header := &types.Header{Number: big.NewInt(100), Difficulty: big.NewInt(0), GasLimit: 30000000, Time: uint64(time.Now().Unix())}
	receipt := &types.Receipt{Type: types.LegacyTxType, Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 21000, GasUsed: 21000, TxHash: transaction.Hash(), Logs: []*types.Log{}}
	block := types.NewBlock(header, &types.Body{Transactions: []*types.Transaction{transaction}}, []*types.Receipt{receipt}, trie.NewStackTrie(nil))
	receipt.BlockNumber, receipt.BlockHash = block.Number(), block.Hash()
	var brokenReceipts atomic.Int64
	server := func(broken bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				ID     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			var value any
			switch req.Method {
			case "eth_chainId":
				value = "0xaa36a7"
			case "eth_getBlockByNumber":
				if string(req.Params[0]) == `"latest"` {
					latest := *block.Header()
					latest.Number = big.NewInt(102)
					value = &latest
				} else if string(req.Params[1]) == "true" {
					body, err := json.Marshal(block.Header())
					require.NoError(t, err)
					var object map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(body, &object))
					txJSON, err := json.Marshal(transaction)
					require.NoError(t, err)
					var txObject map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(txJSON, &txObject))
					txObject["blockHash"], _ = json.Marshal(block.Hash())
					txObject["blockNumber"], _ = json.Marshal(hexutil.EncodeBig(block.Number()))
					txObject["transactionIndex"] = json.RawMessage(`"0x0"`)
					object["transactions"], _ = json.Marshal([]map[string]json.RawMessage{txObject})
					object["uncles"] = json.RawMessage(`[]`)
					value = object
				} else {
					value = block.Header()
				}
			case "eth_getTransactionReceipt":
				if broken {
					brokenReceipts.Add(1)
					_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unsupported"}}`, req.ID)
					return
				}
				value = receipt
			case "eth_getLogs":
				value = []types.Log{}
			default:
				t.Errorf("unexpected method %s", req.Method)
				return
			}
			if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value}); err != nil {
				t.Error(err)
			}
		}))
	}
	primary, secondary, broken := server(false), server(false), server(true)
	defer primary.Close()
	defer secondary.Close()
	defer broken.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "ETHEREUM-SEPOLIA", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second, Endpoints: []chainconfig.Endpoint{{ProviderID: "first", URL: primary.URL}, {ProviderID: "second", Priority: 1, URL: secondary.URL}, {ProviderID: "broken", Priority: 2, URL: broken.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	driver := &Driver{network: crypto.NetworkRef{Code: "ETHEREUM-SEPOLIA"}, rpc: pool}
	result, err := driver.CheckIngestionRPC(t.Context(), nil, crypto.IngestionRPCCheckOptions{Samples: 1})
	require.NoError(t, err)
	require.True(t, result.Ready)
	require.True(t, result.Providers[0].Ready)
	require.True(t, result.Providers[1].Ready)
	require.False(t, result.Providers[2].Ready)
	require.Equal(t, int64(1), brokenReceipts.Load())
	require.Equal(t, 1, result.Providers[2].Operations[2].Errors["unsupported"])
	require.Equal(t, 1, result.Providers[0].Operations[2].Successful)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = driver.CheckIngestionRPC(cancelled, nil, crypto.IngestionRPCCheckOptions{Samples: 1})
	require.Error(t, err)
}
