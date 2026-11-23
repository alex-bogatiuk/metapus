package evm

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestNetworkFeeProofRequiresIndependentExactEVMQuorum(t *testing.T) {
	for _, mode := range []string{"success", "reverted", "zero", "blob", "one_source", "same_group", "wrong_fee", "unknown_fee_field", "missing_price", "missing_status", "null_status", "unsupported_chain", "wrong_expected_price", "wrong_cumulative_gas", "wrong_sender", "wrong_block", "wrong_tx_block", "head_divergence"} {
		t.Run(mode, func(t *testing.T) {
			key, err := gethcrypto.GenerateKey()
			require.NoError(t, err)
			chain := big.NewInt(11155111)
			to := common.HexToAddress("0x0000000000000000000000000000000000000002")
			tx := gethtypes.NewTx(&gethtypes.DynamicFeeTx{ChainID: chain, Nonce: 4, To: &to, Value: big.NewInt(0), Gas: 21000, GasFeeCap: big.NewInt(100), GasTipCap: big.NewInt(0)})
			if mode == "blob" {
				tx = gethtypes.NewTx(&gethtypes.BlobTx{ChainID: uint256.NewInt(11155111), Nonce: 4, To: to, Value: uint256.NewInt(0), Gas: 21000, GasFeeCap: uint256.NewInt(100), GasTipCap: uint256.NewInt(0), BlobFeeCap: uint256.NewInt(20), BlobHashes: []common.Hash{common.HexToHash("0x01" + strings.Repeat("0", 62))}})
			}
			tx, err = gethtypes.SignTx(tx, gethtypes.LatestSignerForChainID(chain), key)
			require.NoError(t, err)
			header := &gethtypes.Header{Number: big.NewInt(100), Difficulty: big.NewInt(0), GasLimit: 30000000, GasUsed: 21000, Time: uint64(time.Now().Unix()), BaseFee: big.NewInt(1)}
			if mode == "zero" {
				header.BaseFee = big.NewInt(0)
			}
			server := func(index int) *httptest.Server {
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
						h := *header
						var tag string
						require.NoError(t, json.Unmarshal(req.Params[0], &tag))
						if tag == "latest" || tag == "0x78" || tag == "finalized" {
							h.Number = big.NewInt(120)
							if mode == "head_divergence" && index == 1 {
								h.Root = common.HexToHash("0xaa")
							}
						}
						result = &h
					case "eth_getTransactionReceipt":
						price := big.NewInt(1)
						if mode == "zero" {
							price = big.NewInt(0)
						}
						if mode == "wrong_expected_price" {
							price = big.NewInt(3)
						}
						if mode == "wrong_fee" && index == 1 {
							price = big.NewInt(2)
						}
						receipt := &gethtypes.Receipt{Type: tx.Type(), Status: 1, CumulativeGasUsed: 21000, GasUsed: 21000, EffectiveGasPrice: price, TxHash: tx.Hash(), BlockHash: header.Hash(), BlockNumber: big.NewInt(100), Logs: []*gethtypes.Log{}}
						if mode == "wrong_cumulative_gas" {
							receipt.CumulativeGasUsed = 1
						}
						if mode == "reverted" {
							receipt.Status = 0
						}
						if mode == "wrong_block" {
							receipt.BlockHash = common.HexToHash("0xaa")
						}
						if mode == "blob" {
							receipt.BlobGasUsed = tx.BlobGas()
							receipt.BlobGasPrice = big.NewInt(2)
						}
						raw, err := json.Marshal(receipt)
						require.NoError(t, err)
						var fields map[string]json.RawMessage
						require.NoError(t, json.Unmarshal(raw, &fields))
						if mode == "unknown_fee_field" {
							fields["l1Fee"] = json.RawMessage(`"0x2"`)
						}
						if mode == "missing_price" {
							delete(fields, "effectiveGasPrice")
						}
						if mode == "missing_status" {
							delete(fields, "status")
						}
						if mode == "null_status" {
							fields["status"] = json.RawMessage(`null`)
						}
						if mode == "wrong_sender" {
							fields["from"] = json.RawMessage(`"0x0000000000000000000000000000000000000009"`)
						}
						result = fields
					case "eth_getTransactionByHash":
						raw, err := tx.MarshalJSON()
						require.NoError(t, err)
						var fields map[string]json.RawMessage
						require.NoError(t, json.Unmarshal(raw, &fields))
						block := header.Hash().Hex()
						if mode == "wrong_tx_block" {
							block = common.HexToHash("0xaa").Hex()
						}
						fields["blockHash"], err = json.Marshal(block)
						require.NoError(t, err)
						fields["blockNumber"] = json.RawMessage(`"0x64"`)
						fields["transactionIndex"] = json.RawMessage(`"0x0"`)
						result = fields
					default:
						t.Errorf("unexpected fee RPC %s", req.Method)
					}
					w.Header().Set("Content-Type", "application/json")
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}))
				}))
			}
			a, b := server(0), server(1)
			defer a.Close()
			defer b.Close()
			endpoints := []chainconfig.Endpoint{{ProviderID: "one", TrustGroup: "one", URL: a.URL}, {ProviderID: "two", TrustGroup: "two", URL: b.URL}}
			if mode == "one_source" {
				endpoints = endpoints[:1]
			}
			if mode == "same_group" {
				endpoints[1].TrustGroup = "one"
			}
			pool, err := NewRPCPool(context.Background(), RPCPoolConfig{NetworkCode: "TEST", ExpectedChainID: chain, Endpoints: endpoints, FailureThreshold: 2, OpenDuration: time.Second})
			require.NoError(t, err)
			defer pool.Close()
			verifier := &TransferVerifier{network: crypto.NetworkRef{ID: id.New(), Family: crypto.ChainFamilyEVM, NetworkIdentifier: "11155111", NativeTokenID: id.New(), NativeDecimals: 18, RequiredConfirmations: 19}, rpc: pool}
			if mode == "unsupported_chain" {
				verifier.network.NetworkIdentifier = "10"
			}
			receipt, err := verifier.VerifyNetworkFee(t.Context(), tx.Hash().Hex())
			if mode != "success" && mode != "reverted" && mode != "zero" && mode != "blob" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, receipt.FeeProof)
			require.NoError(t, receipt.FeeProof.ValidateReceipt(receipt))
			require.Equal(t, strings.ToLower(gethcrypto.PubkeyToAddress(key.PublicKey).Hex()), receipt.FeeProof.PayerKey())
			expected := "21000"
			if mode == "zero" {
				expected = "0"
			}
			if mode == "blob" {
				expected = "283144"
			}
			require.Equal(t, expected, receipt.Fee.TotalFee.String())
			if mode == "reverted" {
				require.Equal(t, crypto.ReceiptReverted, receipt.Status)
			}
		})
	}
}
