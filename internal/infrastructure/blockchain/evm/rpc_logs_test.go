package evm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/providerpolicy"
)

type logsRequest struct {
	ID     json.RawMessage `json:"id"`
	Params []struct {
		FromBlock hexutil.Uint64   `json:"fromBlock"`
		ToBlock   hexutil.Uint64   `json:"toBlock"`
		Address   []common.Address `json:"address"`
		Topics    [][]common.Hash  `json:"topics"`
	} `json:"params"`
}

func logsTestServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, logsRequest)) *httptest.Server {
	t.Helper()
	base := blockTestServer(t, nil)
	t.Cleanup(base.Close)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var method struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &method); err != nil {
			t.Error(err)
			return
		}
		if method.Method != "eth_getLogs" {
			r.Body = io.NopCloser(bytes.NewReader(body))
			base.Config.Handler.ServeHTTP(w, r)
			return
		}
		var request logsRequest
		if err := json.Unmarshal(body, &request); err != nil || len(request.Params) != 1 {
			t.Errorf("invalid logs request: %s", body)
			return
		}
		handler(w, r, request)
	}))
	t.Cleanup(s.Close)
	return s
}

func rangeRejection(w http.ResponseWriter, request logsRequest, status int) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{
		"code": -32600, "message": "Under the Free tier plan, you can make eth_getLogs requests with up to a 10 block range. URL https://rpc.invalid/private-key",
	}})
}

func logsTestPool(t *testing.T, servers ...*httptest.Server) *RPCPool {
	t.Helper()
	var endpoints []chainconfig.Endpoint
	for i, s := range servers {
		endpoints = append(endpoints, chainconfig.Endpoint{ProviderID: fmt.Sprintf("provider-%d", i), URL: s.URL, Priority: i})
	}
	p, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Settings: chainconfig.EVMSettings{MaxAttempts: 2, RequestsPerSecond: 1000}, Endpoints: endpoints})
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return p
}

func TestLogsLearnsProviderRangeLimitAndPreservesExactCoverage(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			address := common.HexToAddress("0x01")
			topics := [][]common.Hash{{common.HexToHash("0x02")}, nil, {common.HexToHash("0x03"), common.HexToHash("0x04")}}
			var rejected atomic.Int32
			server := logsTestServer(t, func(w http.ResponseWriter, r *http.Request, request logsRequest) {
				q := request.Params[0]
				require.Equal(t, []common.Address{address}, q.Address)
				require.Equal(t, topics, q.Topics)
				if q.ToBlock-q.FromBlock+1 > 10 {
					rejected.Add(1)
					rangeRejection(w, request, status)
					return
				}
				var logs []types.Log
				for height := uint64(q.FromBlock); height <= uint64(q.ToBlock); height++ {
					logs = append(logs, types.Log{BlockNumber: height, Address: address, Topics: topics[0], Data: []byte{}, TxHash: common.BigToHash(new(big.Int).SetUint64(height))})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": logs})
			})
			pool := logsTestPool(t, server)
			for range 2 {
				query := ethereum.FilterQuery{FromBlock: big.NewInt(101), ToBlock: big.NewInt(200), Addresses: []common.Address{address}, Topics: topics}
				logs, err := pool.FilterLogs(t.Context(), query)
				require.NoError(t, err)
				require.Len(t, logs, 100)
				for i, log := range logs {
					require.EqualValues(t, 101+i, log.BlockNumber)
				}
				require.EqualValues(t, 101, query.FromBlock.Int64())
				require.EqualValues(t, 200, query.ToBlock.Int64())
			}
			require.EqualValues(t, 1, rejected.Load(), "the learned limit avoids repeating the rejected range")
		})
	}
}

func TestLogsDoesNotSplitUnrelatedInvalidRequest(t *testing.T) {
	var calls atomic.Int32
	server := logsTestServer(t, func(w http.ResponseWriter, r *http.Request, request logsRequest) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32602, "message": "invalid topic"}})
	})
	pool := logsTestPool(t, server)
	_, err := pool.FilterLogs(t.Context(), ethereum.FilterQuery{FromBlock: big.NewInt(1), ToBlock: big.NewInt(100)})
	require.ErrorIs(t, err, ErrRPCInvalidRequest)
	require.EqualValues(t, 1, calls.Load())
	require.NotContains(t, err.Error(), server.URL)
}

func TestLogsFailedSubrangeDiscardsResultsAndDoesNotAdvanceCheckpoint(t *testing.T) {
	server := logsTestServer(t, func(w http.ResponseWriter, r *http.Request, request logsRequest) {
		q := request.Params[0]
		if q.ToBlock-q.FromBlock+1 > 10 {
			rangeRejection(w, request, http.StatusBadRequest)
			return
		}
		if q.FromBlock >= 11 {
			http.Error(w, "failure with private-key", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": []types.Log{{BlockNumber: uint64(q.FromBlock), Data: []byte{}, Topics: []common.Hash{}}}})
	})
	pool := logsTestPool(t, server)
	logs, err := pool.FilterLogs(t.Context(), ethereum.FilterQuery{FromBlock: big.NewInt(1), ToBlock: big.NewInt(100)})
	require.Error(t, err)
	require.Empty(t, logs, "a failed later subrange must discard an already received prefix")
	pool = logsTestPool(t, server)
	asset := crypto.AssetRef{TokenID: id.New(), Standard: "ERC-20", ContractAddress: common.HexToAddress("0x01").Hex(), Decimals: 6}
	stateRepo, historyRepo := &fakeWatcherStateRepo{}, &fakeWatcherHistoryRepo{}
	watcher, err := NewWatcher(WatcherConfig{Network: testSepoliaNetwork(), RPC: pool, StateRepository: stateRepo, BlockHistoryRepository: historyRepo, BlockCommitter: &fakeBlockCommitter{stateRepo: stateRepo, historyRepo: historyRepo}, NetworkHoldRecorder: &fakeNetworkHoldRepo{}, WatcherKey: "erc20:test", Asset: &asset, Addresses: []string{common.HexToAddress("0x02").Hex()}})
	require.NoError(t, err)
	state := &crypto.WatcherState{NetworkID: watcher.network.ID, WatcherKey: watcher.watcherKey}
	err = watcher.poll(t.Context(), state, noopSink())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-key")
	require.Zero(t, state.LastBlock)
	require.Zero(t, stateRepo.saves)
	require.Zero(t, historyRepo.saves)
}

func TestRPCRecoveryProbesLeaveAttemptForHealthyThirdProvider(t *testing.T) {
	var failed [2]atomic.Int32
	var servers []*httptest.Server
	for i := range 2 {
		servers = append(servers, logsTestServer(t, func(w http.ResponseWriter, r *http.Request, request logsRequest) {
			failed[i].Add(1)
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
	}
	servers = append(servers, logsTestServer(t, func(w http.ResponseWriter, r *http.Request, request logsRequest) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": []types.Log{}})
	}))
	pool := logsTestPool(t, servers...)
	for range 4 {
		for _, endpoint := range pool.endpoints[:2] {
			endpoint.mu.Lock()
			endpoint.operations[opLogs] = &operationCircuit{Failures: 1, OpenUntil: time.Now().Add(-time.Second)}
			endpoint.mu.Unlock()
		}
		_, err := pool.FilterLogs(t.Context(), ethereum.FilterQuery{FromBlock: big.NewInt(1), ToBlock: big.NewInt(2)})
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, failed[0].Load())
	require.EqualValues(t, 2, failed[1].Load())
}

func TestLogsSubrangesShareAttemptDeadline(t *testing.T) {
	var calls atomic.Int32
	server := logsTestServer(t, func(w http.ResponseWriter, r *http.Request, request logsRequest) {
		q := request.Params[0]
		if q.ToBlock-q.FromBlock+1 > 10 {
			rangeRejection(w, request, http.StatusBadRequest)
			return
		}
		calls.Add(1)
		<-r.Context().Done()
	})
	pool := logsTestPool(t, server)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	logs, err := pool.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: big.NewInt(1), ToBlock: big.NewInt(100)})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, logs)
	require.EqualValues(t, 1, calls.Load())
}

func TestLogsSubrangesRespectSharedRequestRate(t *testing.T) {
	var calls atomic.Int32
	server := logsTestServer(t, func(w http.ResponseWriter, r *http.Request, request logsRequest) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": []types.Log{}})
	})
	pool := logsTestPool(t, server)
	endpoint := pool.endpoints[0]
	endpoint.logBlockRange = 10
	limited, err := providerpolicy.Acquire(server.URL+"/separate-test-quota", providerpolicy.Capacity{Concurrency: 2, BulkConcurrency: 1, RequestsPerSecond: 1, MinRequestIntervalMS: 1000})
	require.NoError(t, err)
	defer providerpolicy.Release(limited)
	original := endpoint.admission
	endpoint.admission = limited
	defer func() { endpoint.admission = original }()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	logs, err := pool.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: big.NewInt(1), ToBlock: big.NewInt(100)})
	require.ErrorIs(t, err, ErrRPCAdmissionBusy)
	require.Empty(t, logs)
	require.EqualValues(t, 1, calls.Load(), "extra subrequests must consume the same endpoint quota")
}
