package evm

import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"math/big"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransferVerificationRequiresIndependentExactFinalEVMQuorum(t *testing.T) {
	for _, scenario := range []string{"native", "erc20", "contract_code", "duplicate_log", "same_group", "one_source", "wrong_amount", "reverted", "wrong_receipt_block", "head_divergence", "pending", "wrong_log", "low_quota_route", "low_quota_fallback", "missing_receipts", "mismatch_with_reserve"} {
		t.Run(scenario, func(t *testing.T) {
			key, err := gethcrypto.GenerateKey()
			require.NoError(t, err)
			chain := big.NewInt(11155111)
			from := gethcrypto.PubkeyToAddress(key.PublicKey)
			to := common.HexToAddress("0x0000000000000000000000000000000000000002")
			contract := common.HexToAddress("0x0000000000000000000000000000000000000003")
			tx, err := gethtypes.SignTx(gethtypes.NewTx(&gethtypes.DynamicFeeTx{ChainID: chain, Nonce: 4, To: &to, Value: big.NewInt(12), Gas: 21000, GasFeeCap: big.NewInt(100), GasTipCap: big.NewInt(1)}), gethtypes.LatestSignerForChainID(chain), key)
			require.NoError(t, err)
			header := &gethtypes.Header{Number: big.NewInt(100), Difficulty: big.NewInt(0), GasLimit: 30000000, Time: uint64(time.Now().Unix()), BaseFee: big.NewInt(1)}
			event := crypto.BlockchainEvent{NetworkID: id.New(), TxHash: tx.Hash().Hex(), EventKind: crypto.EventKindNativeValue, FromAddress: from.Hex(), ToAddress: to.Hex(), Amount: types.MustRawAmount("12"), BlockNumber: 100, BlockHash: header.Hash().Hex(), Direction: crypto.TransferDirectionInternal}
			token := scenario == "erc20" || scenario == "duplicate_log" || scenario == "wrong_log"
			if token {
				event.EventKind = crypto.EventKindContractLog
				event.TokenContract = contract.Hex()
				event.EventIndex = 7
			}
			if scenario == "wrong_amount" {
				event.Amount = types.MustRawAmount("13")
			}
			var calls [3]atomic.Int64
			server := func(index int) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls[index].Add(1)
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
						if tag == "latest" || tag == "0x78" {
							h.Number = big.NewInt(120)
							if scenario == "pending" {
								h.Number = big.NewInt(101)
							}
							if scenario == "head_divergence" && index == 1 {
								h.Root = common.HexToHash("0xab")
							}
						}
						result = &h
					case "eth_getTransactionReceipt":
						receipt := &gethtypes.Receipt{Type: 2, Status: 1, CumulativeGasUsed: 21000, GasUsed: 21000, TxHash: tx.Hash(), BlockHash: header.Hash(), BlockNumber: big.NewInt(100), Logs: []*gethtypes.Log{}}
						if scenario == "reverted" {
							receipt.Status = 0
						}
						if scenario == "wrong_receipt_block" || (scenario == "mismatch_with_reserve" && index == 0) {
							receipt.BlockHash = common.HexToHash("0xaa")
						}
						if token {
							log := &gethtypes.Log{Address: contract, Topics: []common.Hash{erc20ABI.Events["Transfer"].ID, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())}, Data: common.LeftPadBytes(big.NewInt(12).Bytes(), 32), BlockNumber: 100, BlockHash: header.Hash(), TxHash: tx.Hash(), Index: 7}
							if scenario == "wrong_log" {
								log.Data = common.LeftPadBytes(big.NewInt(13).Bytes(), 32)
							}
							receipt.Logs = append(receipt.Logs, log)
							if scenario == "duplicate_log" {
								other := *log
								other.Index = 8
								receipt.Logs = append(receipt.Logs, &other)
							}
						}
						result = receipt
						if scenario == "missing_receipts" {
							result = nil
						}
					case "eth_getTransactionByHash":
						raw, err := tx.MarshalJSON()
						require.NoError(t, err)
						var data map[string]any
						require.NoError(t, json.Unmarshal(raw, &data))
						data["blockHash"] = header.Hash().Hex()
						data["blockNumber"] = "0x64"
						data["transactionIndex"] = "0x0"
						result = data
					case "eth_getCode":
						result = "0x"
						if scenario == "contract_code" {
							result = "0x6000"
						}
					default:
						t.Errorf("unexpected RPC %s", req.Method)
					}
					w.Header().Set("Content-Type", "application/json")
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}))
				}))
			}
			first, second := server(0), server(1)
			defer first.Close()
			defer second.Close()
			endpoints := []chainconfig.Endpoint{{ProviderID: "one", TrustGroup: "one", URL: first.URL}, {ProviderID: "two", TrustGroup: "two", URL: second.URL}}
			if scenario == "same_group" {
				endpoints[1].TrustGroup = "one"
			}
			if scenario == "one_source" {
				endpoints = endpoints[:1]
			}
			if scenario == "low_quota_route" {
				endpoints[1].MinRequestIntervalMS = 30000
			}
			if scenario == "low_quota_fallback" || scenario == "mismatch_with_reserve" {
				third := server(2)
				defer third.Close()
				endpoints = append(endpoints, chainconfig.Endpoint{ProviderID: "three", TrustGroup: "three", URL: third.URL})
				if scenario == "low_quota_fallback" {
					endpoints[0].MinRequestIntervalMS = 30000
				}
			}
			pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "TEST", ExpectedChainID: chain, Endpoints: endpoints, FailureThreshold: 2, OpenDuration: time.Second})
			require.NoError(t, err)
			defer pool.Close()
			driver := &Driver{network: crypto.NetworkRef{ID: event.NetworkID, NetworkIdentifier: chain.String(), RequiredConfirmations: 12}, rpc: pool}
			before := [3]int64{calls[0].Load(), calls[1].Load(), calls[2].Load()}
			proof, err := driver.VerifyTransfer(t.Context(), event)
			if token && (scenario == "erc20" || scenario == "duplicate_log") {
				candidate := event
				candidate.EventIndex = 0
				located, locateErr := driver.VerifyExpectedTransfer(t.Context(), candidate)
				if scenario == "duplicate_log" {
					require.ErrorIs(t, locateErr, crypto.ErrVerificationMismatch)
				} else {
					require.NoError(t, locateErr)
					require.EqualValues(t, 7, located.Event.EventIndex)
					require.NotNil(t, located.AccountingIdentity)
				}
			}
			switch scenario {
			case "native", "erc20", "contract_code", "duplicate_log", "low_quota_fallback":
				require.NoError(t, err)
				require.NoError(t, proof.Validate())
				if scenario == "low_quota_fallback" {
					require.Equal(t, before[0], calls[0].Load())
				}
				want := 1
				if scenario == "contract_code" {
					want = 0
				}
				if scenario == "duplicate_log" {
					want = 2
				}
				require.Equal(t, want, proof.MatchingEffects)
				require.Equal(t, int64(4), *proof.Event.SourceNonce)
			case "low_quota_route":
				require.ErrorContains(t, err, "provider two operation verification: budget_unavailable")
				require.Equal(t, before[1], calls[1].Load(), "ineligible reserve must receive no proof requests")
			case "missing_receipts":
				require.ErrorContains(t, err, "provider one operation receipt: not_found")
				require.ErrorContains(t, err, "provider two operation receipt: not_found")
			case "mismatch_with_reserve":
				require.True(t, errors.Is(err, crypto.ErrVerificationMismatch))
				require.Equal(t, before[1], calls[1].Load(), "a semantic mismatch must not shop for another source")
				require.Equal(t, before[2], calls[2].Load())
			default:
				require.Error(t, err)
			}
		})
	}
}
