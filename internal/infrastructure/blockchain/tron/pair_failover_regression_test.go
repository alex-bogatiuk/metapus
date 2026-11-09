package tron

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

const (
	simHealthy int64 = iota
	simOffline
	simTimeout
	simReceiptOffline
	simMissingReceipt
	simDifferentBlock
	simDifferentReceipt
	simUnknownTrace
	simRateLimited
)

type simulatedRPC struct {
	server    *httptest.Server
	head      atomic.Int64
	fault     atomic.Int64
	freshTime atomic.Bool
	blocks    atomic.Int64
	receipts  atomic.Int64
}

func newSimulatedRPC(t *testing.T, epoch int64) *simulatedRPC {
	t.Helper()
	n := &simulatedRPC{}
	n.head.Store(190)
	block, receipts := blockFixture(t, 151, false)
	block.BlockHeader.RawData.Timestamp = epoch + (151-200)*3000
	receipts[0].BlockTimestamp = block.BlockHeader.RawData.Timestamp
	unknown := traceFixture(t, "unrecognizedOperation", "d", 23)
	n.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fault := n.fault.Load()
		if fault == simOffline || fault == simRateLimited {
			code := http.StatusServiceUnavailable
			if fault == simRateLimited {
				code = http.StatusTooManyRequests
			}
			http.Error(w, "temporarily unavailable", code)
			return
		}
		if fault == simTimeout {
			<-r.Context().Done()
			return
		}
		var request struct {
			Num int64 `json:"num"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "getnowblock"):
			height := n.head.Load()
			if strings.HasPrefix(r.URL.Path, "/wallet/") {
				height += 10
			}
			head := &BlockInfo{BlockID: fmt.Sprintf("%064x", height)}
			head.BlockHeader.RawData.Number = height
			head.BlockHeader.RawData.Timestamp = epoch + (height-200)*3000
			if n.freshTime.Load() {
				head.BlockHeader.RawData.Timestamp = epoch
			}
			head.BlockHeader.RawData.ParentHash = fmt.Sprintf("%064x", height-1)
			_ = json.NewEncoder(w).Encode(head)
		case strings.HasSuffix(r.URL.Path, "getblockbynum"):
			n.blocks.Add(1)
			if request.Num > n.head.Load()+10 {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			if request.Num == 151 {
				copy := *block
				if fault == simDifferentBlock {
					copy.BlockID = strings.Repeat("f", 64)
				}
				_ = json.NewEncoder(w).Encode(&copy)
				return
			}
			other := &BlockInfo{BlockID: fmt.Sprintf("%064x", request.Num)}
			other.BlockHeader.RawData.Number = request.Num
			other.BlockHeader.RawData.ParentHash = fmt.Sprintf("%064x", request.Num-1)
			other.BlockHeader.RawData.Timestamp = epoch + (request.Num-200)*3000
			if fault == simMissingReceipt {
				other.Transactions = block.Transactions
			}
			_ = json.NewEncoder(w).Encode(other)
		case strings.HasSuffix(r.URL.Path, "gettransactioninfobyblocknum"):
			n.receipts.Add(1)
			if fault == simReceiptOffline {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			if fault == simMissingReceipt {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			if request.Num != 151 {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			copy := append([]TransactionInfo(nil), receipts...)
			if fault == simDifferentReceipt {
				copy[0].Fee++
			}
			if fault == simUnknownTrace {
				copy[0].InternalTransactions = []InternalTransaction{unknown}
			}
			_ = json.NewEncoder(w).Encode(copy)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(n.server.Close)
	return n
}

// Actual routing and financial proof validation, with independent local RPCs.
func TestRPCFailoverRegressionMatrix(t *testing.T) {
	type scenario struct {
		name    string
		count   int
		setup   string
		wanted  bool
		current bool
		gap     bool
	}
	cases := []scenario{
		{"all_three_fresh", 3, "healthy", true, true, false},
		{"trongrid_http_503", 3, "primary_offline", true, true, false},
		{"trongrid_timeout", 3, "primary_timeout", true, true, false},
		{"trongrid_http_429", 3, "primary_rate_limited", true, true, false},
		{"chainstack_receipt_http_503", 3, "receipt_offline", true, true, false},
		{"chainstack_marked_lagging", 3, "lagging", true, true, false},
		{"chainstack_sudden_head_regression", 3, "regression", true, true, false},
		{"one_fresh_provider_remaining", 3, "one_fresh", false, false, false},
		{"four_providers_one_offline_one_lagging", 4, "two_faults", true, true, false},
		{"four_providers_lagging_still_selected", 4, "lagging", true, true, false},
		{"chainstack_catches_up_then_rejoins", 3, "recovered", true, true, false},
		{"head_recovers_but_receipts_still_missing", 3, "head_only_recovery", true, true, false},
		{"different_urls_same_trust_group", 3, "same_group", false, false, false},
		{"block_mismatch_does_not_shop_for_third", 3, "block_mismatch", false, false, false},
		{"receipt_mismatch_does_not_shop_for_third", 3, "receipt_mismatch", false, false, false},
		{"unknown_trace_does_not_shop_for_third", 3, "unknown_trace", false, false, false},
		{"missing_receipt_keeps_verification_pending", 3, "missing_receipt", false, false, false},
		{"fourth_fullnode_only_is_not_financial_reserve", 4, "fullnode_only", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			epoch := time.Now().UnixMilli()
			labels := []string{"trongrid", "chainstack", "nileex", "getblock_candidate"}
			nodes := make([]*simulatedRPC, tc.count)
			endpoints := make([]chainconfig.Endpoint, tc.count)
			for i := range nodes {
				nodes[i] = newSimulatedRPC(t, epoch)
				group := labels[i]
				if tc.setup == "same_group" {
					group = "shared_backend"
				}
				endpoints[i] = chainconfig.Endpoint{ProviderID: labels[i], TrustGroup: group, URL: nodes[i].server.URL}
			}
			switch tc.setup {
			case "primary_offline":
				nodes[0].fault.Store(simOffline)
			case "primary_timeout":
				nodes[0].fault.Store(simTimeout)
			case "primary_rate_limited":
				nodes[0].fault.Store(simRateLimited)
			case "receipt_offline":
				nodes[1].fault.Store(simReceiptOffline)
			case "lagging", "one_fresh", "two_faults", "fullnode_only", "recovered", "head_only_recovery":
				nodes[1].head.Store(100)
			case "block_mismatch":
				nodes[1].fault.Store(simDifferentBlock)
			case "receipt_mismatch":
				nodes[1].fault.Store(simDifferentReceipt)
			case "unknown_trace":
				nodes[1].fault.Store(simUnknownTrace)
			case "missing_receipt":
				nodes[1].fault.Store(simMissingReceipt)
			}
			if tc.setup == "one_fresh" || tc.setup == "two_faults" || tc.setup == "fullnode_only" {
				nodes[0].fault.Store(simOffline)
			}
			solidity := endpoints
			if tc.setup == "fullnode_only" {
				solidity = endpoints[:3]
			}
			pool, err := NewRPCPool(RPCPoolConfig{NetworkCode: "SIMULATION", FullNode: endpoints, Solidity: solidity, Settings: chainconfig.TRONSettings{IngestionMode: "hybrid", AttemptTimeoutMS: 200, ReadBudgetMS: 2500, MaxAttempts: 1, RequestsPerSecond: 1000}})
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			if tc.setup == "lagging" || tc.setup == "one_fresh" || tc.setup == "two_faults" || tc.setup == "fullnode_only" || tc.setup == "recovered" || tc.setup == "head_only_recovery" {
				pool.Probe(t.Context())
			}
			if tc.setup == "regression" {
				_, _, err = pool.ReadAgreedBlock(t.Context(), 151)
				require.NoError(t, err)
				nodes[1].head.Store(100)
			}
			if tc.setup == "recovered" || tc.setup == "head_only_recovery" {
				nodes[1].head.Store(190)
				if tc.setup == "head_only_recovery" {
					nodes[1].fault.Store(simMissingReceipt)
				}
				pool.Probe(t.Context())
				if tc.setup == "recovered" {
					recoveredAt := time.UnixMilli(epoch).Add(6 * time.Second)
					pool.now = func() time.Time { return recoveredAt }
					nodes[1].head.Store(192)
					pool.Probe(t.Context())
				}
			}
			for _, node := range nodes {
				node.blocks.Store(0)
				node.receipts.Store(0)
			}
			started := time.Now()
			data, sources, readErr := pool.ReadAgreedBlock(context.Background(), 151)
			success := readErr == nil
			actualSources := []string{}
			for _, s := range sources {
				actualSources = append(actualSources, s.ProviderID)
			}
			if tc.setup == "recovered" {
				require.Contains(t, actualSources, "chainstack", "complete recovery must admit the original provider again")
			}
			if success {
				require.NotEmpty(t, data.Digest)
				require.Len(t, sources, 2)
				require.NotEqual(t, sources[0].TrustGroup, sources[1].TrustGroup)
				require.Equal(t, sources[0].Digest, sources[1].Digest)
				events, decodeErr := decodeTransfers(crypto.NetworkRef{ID: id.New()}, data, crypto.FinalitySolidified, int(sources[0].HeadHeight-151)+1)
				require.NoError(t, decodeErr)
				require.NotEmpty(t, events)
				for _, event := range events {
					proof := crypto.TransferVerification{Event: event, Sources: sources, DecoderVersion: 2, VerifiedAt: time.Now().UTC(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded}
					require.NoError(t, proof.Validate(), "successful route must retain full independent transfer verification")
				}
			}
			calls := map[string]int64{}
			for i, node := range nodes {
				calls[labels[i]] = node.blocks.Load() + node.receipts.Load()
			}
			result := map[string]any{"scenario": tc.name, "providers": tc.count, "wantedSuccess": tc.wanted, "actualSuccess": success, "knownAvailabilityGap": tc.gap, "errorCategory": blockErrorCategory(readErr), "sources": actualSources, "blockAndReceiptCalls": calls, "durationMs": time.Since(started).Milliseconds()}
			if readErr != nil {
				result["error"] = readErr.Error()
			}
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			t.Log("SIM_RESULT " + string(raw))
			require.Equal(t, tc.current, success, "healthy independent reserves must preserve availability")
			require.Equal(t, tc.gap, tc.wanted != success)
			if strings.Contains(tc.setup, "mismatch") || tc.setup == "unknown_trace" || tc.setup == "missing_receipt" {
				require.Zero(t, calls["nileex"], "no provider shopping to hide inconsistent/partial data")
			}
		})
	}
}
