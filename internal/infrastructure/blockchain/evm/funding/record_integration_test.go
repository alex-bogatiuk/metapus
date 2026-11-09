//go:build integration

package funding

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/blockchain/evm"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestEVMFundingUsesPersistedIndependentProofAndRecognition(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_CRYPTO_DSN is required")
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	tm := postgres.NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(t.Context(), tm)
	for _, scenario := range []string{"two_sources", "one_source", "disagreement", "recipient_code"} {
		t.Run(scenario, func(t *testing.T) {
			rollback := errors.New("rollback EVM funding fixture")
			err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
				q := tm.GetQuerier(ctx)
				var network, asset id.ID
				var assetCode string
				var required int
				require.NoError(t, q.QueryRow(ctx, `SELECT n.id,t.id,t.code,n.confirmations_needed FROM cat_blockchain_networks n
				JOIN cat_tokens t ON t.network_id=n.id AND t.token_standard='native'
				WHERE n.chain_family='evm' AND n.network_identifier='11155111' LIMIT 1`).Scan(&network, &asset, &assetCode, &required))
				_, err := q.Exec(ctx, "UPDATE cat_blockchain_networks SET is_active=TRUE WHERE id=$1", network)
				require.NoError(t, err)
				_, err = q.Exec(ctx, "UPDATE cat_tokens SET is_active=TRUE WHERE id=$1", asset)
				require.NoError(t, err)
				walletID := id.New()
				code := "EF-" + id.Compact(walletID)
				to := common.BytesToAddress(walletID[:])
				_, err = q.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id)
				VALUES($1,$2,'EVM funding proof fixture',$3,$4,'test-key')`, walletID, code, network, to.Hex())
				require.NoError(t, err)
				chain := big.NewInt(11155111)
				tx := signedFundingTransaction(t, chain, to, big.NewInt(23))
				header := &gethtypes.Header{Number: big.NewInt(100), Difficulty: big.NewInt(0), GasLimit: 30000000, Time: uint64(time.Now().Unix()), BaseFee: big.NewInt(1)}
				serve := func(index int) *httptest.Server {
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
							if tag != "0x64" {
								h.Number = big.NewInt(int64(100 + required + 10))
							}
							result = &h
						case "eth_getTransactionReceipt":
							receipt := &gethtypes.Receipt{Type: 2, Status: 1, CumulativeGasUsed: 21000, GasUsed: 21000, TxHash: tx.Hash(), BlockHash: header.Hash(), BlockNumber: big.NewInt(100), Logs: []*gethtypes.Log{}}
							if scenario == "disagreement" && index == 1 {
								receipt.Status = 0
							}
							result = receipt
						case "eth_getTransactionByHash":
							raw, err := tx.MarshalJSON()
							require.NoError(t, err)
							var data map[string]any
							require.NoError(t, json.Unmarshal(raw, &data))
							data["blockHash"], data["blockNumber"], data["transactionIndex"] = header.Hash().Hex(), "0x64", "0x0"
							result = data
						case "eth_getCode":
							result = "0x"
							if scenario == "recipient_code" {
								result = "0x6000"
							}
						default:
							t.Errorf("unexpected RPC %s", req.Method)
						}
						w.Header().Set("Content-Type", "application/json")
						require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}))
					}))
				}
				one, two := serve(0), serve(1)
				defer one.Close()
				defer two.Close()
				endpoints := []chainconfig.Endpoint{{ProviderID: "one", TrustGroup: "one", URL: one.URL}, {ProviderID: "two", TrustGroup: "two", URL: two.URL}}
				if scenario == "one_source" {
					endpoints = endpoints[:1]
				}
				rpc, err := evm.NewRPCPool(ctx, evm.RPCPoolConfig{NetworkCode: "sepolia", ExpectedChainID: chain, Endpoints: endpoints, FailureThreshold: 2, OpenDuration: time.Second})
				require.NoError(t, err)
				defer rpc.Close()
				result, err := RecordConfirmedWalletFunding(ctx, rpc, chain, code, assetCode, tx.Hash().Hex())
				if scenario != "two_sources" {
					require.Error(t, err)
					if scenario == "one_source" {
						require.ErrorIs(t, err, crypto.ErrChainQuorumUnavailable)
					}
					if scenario == "recipient_code" {
						require.ErrorIs(t, err, crypto.ErrVerificationPending)
					}
					var count int
					require.NoError(t, q.QueryRow(ctx, "SELECT count(*) FROM reg_crypto_balance_movements WHERE wallet_id=$1", walletID).Scan(&count))
					require.Zero(t, count)
					return rollback
				}
				require.NoError(t, err)
				require.False(t, result.Existing)
				_, err = RecordConfirmedWalletFunding(ctx, rpc, chain, code, assetCode, tx.Hash().Hex())
				require.NoError(t, err)
				_, err = q.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
				require.NoError(t, err, "verify the complete deferred accounting set before fixture rollback")
				var proof crypto.TransferVerification
				var raw []byte
				require.NoError(t, q.QueryRow(ctx, `SELECT v.evidence FROM reg_crypto_wallet_fundings f
				JOIN reg_crypto_transfer_verifications v ON v.id=f.verification_id WHERE f.id=$1`, result.ID).Scan(&raw))
				require.NoError(t, json.Unmarshal(raw, &proof))
				require.Len(t, proof.Sources, 2)
				require.NotNil(t, proof.AccountingIdentity)
				var amount string
				require.NoError(t, q.QueryRow(ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", walletID, asset).Scan(&amount))
				require.Equal(t, "23", amount)
				return rollback
			})
			require.ErrorIs(t, err, rollback)
		})
	}
}
