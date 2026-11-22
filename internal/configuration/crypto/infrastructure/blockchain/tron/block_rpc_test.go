package tron

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/pkg/sdk/json"
)

func rpcBlockServer(t *testing.T, block *BlockInfo, receipts []TransactionInfo, batch string, receiptCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "getnowblock"):
			head := *block
			head.Transactions = nil
			if strings.HasPrefix(r.URL.Path, "/wallet/") {
				head.BlockHeader.RawData.Number = 30
				head.BlockID = fmt.Sprintf("%064x", 30)
			}
			_ = json.NewEncoder(w).Encode(head)
		case strings.HasSuffix(r.URL.Path, "getblockbynum"):
			_ = json.NewEncoder(w).Encode(block)
		case strings.HasSuffix(r.URL.Path, "gettransactioninfobyblocknum"):
			if batch == "unsupported" {
				_, _ = w.Write([]byte(`{"Error":"method not supported"}`))
				return
			}
			if batch == "oversize" {
				_, _ = w.Write([]byte(strings.Repeat(" ", 2048)))
				return
			}
			_ = json.NewEncoder(w).Encode(receipts)
		case strings.HasSuffix(r.URL.Path, "gettransactioninfobyid"):
			receiptCalls.Add(1)
			_ = json.NewEncoder(w).Encode(receipts[0])
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAgreedBlockRequiresIndependentCompleteMatchingSources(t *testing.T) {
	for _, scenario := range []string{"verified", "one_rpc", "same_backend", "missing_receipt", "different_amount", "different_hash"} {
		t.Run(scenario, func(t *testing.T) {
			block, receipts := blockFixture(t, 10, false)
			other, otherReceipts := blockFixture(t, 10, false)
			if scenario == "missing_receipt" {
				otherReceipts = nil
			}
			if scenario == "different_amount" {
				other.Transactions[0].RawData.Contract[0].Parameter.Value.Amount++
			}
			if scenario == "different_hash" {
				other.BlockID = strings.Repeat("f", 64)
			}
			var calls atomic.Int32
			first := rpcBlockServer(t, block, receipts, "", &calls)
			defer first.Close()
			second := rpcBlockServer(t, other, otherReceipts, "", &calls)
			defer second.Close()
			urls := []string{first.URL, second.URL}
			if scenario == "one_rpc" {
				urls = urls[:1]
			}
			pool := newTestRPCPool(t, urls, urls)
			pool.settings.IngestionMode = "hybrid"
			pool.settings.RequestsPerSecond = 1000
			for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
				for i, e := range pool.byRole[role] {
					e.trustGroup = fmt.Sprint(i)
					if scenario == "same_backend" {
						e.trustGroup = "same"
					}
				}
			}
			data, sources, err := pool.ReadAgreedBlock(t.Context(), 10)
			switch scenario {
			case "verified":
				require.NoError(t, err)
				require.NotEmpty(t, data.Digest)
				require.Len(t, sources, 2)
				require.Equal(t, int64(30), sources[0].HeadHeight)
			case "one_rpc", "same_backend":
				require.ErrorIs(t, err, crypto.ErrChainQuorumUnavailable)
				_, err = pool.ReadLatestBlock(t.Context(), 10)
				require.NoError(t, err, "one healthy source still discovers payments")
			case "missing_receipt":
				require.ErrorIs(t, err, crypto.ErrVerificationPending)
			default:
				require.ErrorIs(t, err, crypto.ErrVerificationMismatch)
			}
		})
	}
}

func TestBlockReceiptFallbackAndErrorEnvelope(t *testing.T) {
	for _, batch := range []string{"unsupported", "oversize"} {
		t.Run(batch, func(t *testing.T) {
			block, receipts := blockFixture(t, 10, false)
			var calls atomic.Int32
			server := rpcBlockServer(t, block, receipts, batch, &calls)
			defer server.Close()
			pool := newTestRPCPool(t, []string{server.URL}, []string{server.URL})
			if batch == "oversize" {
				for _, e := range pool.byRole[chainconfig.RoleFullNode] {
					e.client.maxResponseBytes = 1024
				}
			}
			_, err := pool.ReadLatestBlock(t.Context(), 10)
			require.NoError(t, err)
			require.EqualValues(t, 1, calls.Load())
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Error":"temporary read failure"}`))
	}))
	defer server.Close()
	client := NewClient(ClientConfig{BaseURL: server.URL})
	_, err := client.GetBlockReceipts(t.Context(), 10, true)
	require.Error(t, err)
}
