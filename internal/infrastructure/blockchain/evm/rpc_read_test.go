package evm

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

// PublicNode's failure mode: valid fast headers and a started full-block
// response whose body never completes. Header success must not heal it.
func TestRPCFullBlockBodyTimeoutDoesNotResetOnHeaderSuccess(t *testing.T) {
	var primaryBlocks atomic.Int64
	primary := blockTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		primaryBlocks.Add(1)
		_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","result":`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	defer primary.Close()
	secondary := blockTestServer(t, nil)
	defer secondary.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "ETHEREUM-SEPOLIA", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute,
		Settings: chainconfig.EVMSettings{AttemptTimeoutMS: 80, ReadBudgetMS: 400}, Endpoints: []chainconfig.Endpoint{{ProviderID: "partial-body", URL: primary.URL}, {ProviderID: "healthy", Priority: 2, URL: secondary.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	started := time.Now()
	block, err := pool.BlockByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, uint64(100), block.NumberU64())
	require.Less(t, time.Since(started), 350*time.Millisecond)
	_, err = pool.HeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	pool.Probe(t.Context())
	_, err = pool.BlockByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, int64(1), primaryBlocks.Load(), "header/runtime probe must not re-enable stalled full-block reads")
}

func TestRuntimeProbeRecoversEndpointUnavailableAtConstruction(t *testing.T) {
	base := blockTestServer(t, nil)
	defer base.Close()
	var primaryAvailable, secondaryAvailable atomic.Bool
	primaryAvailable.Store(true)
	server := func(available *atomic.Bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !available.Load() {
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			base.Config.Handler.ServeHTTP(w, r)
		}))
	}
	primary, secondary := server(&primaryAvailable), server(&secondaryAvailable)
	defer primary.Close()
	defer secondary.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute,
		Endpoints: []chainconfig.Endpoint{{ProviderID: "initial-primary", URL: primary.URL}, {ProviderID: "recovered-secondary", Priority: 1, URL: secondary.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.HeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	primaryAvailable.Store(false)
	secondaryAvailable.Store(true)
	_, err = pool.HeaderByNumber(t.Context(), big.NewInt(100))
	require.Error(t, err, "recovered secondary has not yet verified its chain identity")
	pool.Probe(t.Context())
	header, err := pool.HeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, int64(100), header.Number.Int64())
}
func TestRPCOnlyOneHalfOpenReadAndCancellationDoesNotFailProvider(t *testing.T) {
	server := blockTestServer(t, nil)
	defer server.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Endpoints: []chainconfig.Endpoint{{ProviderID: "test", URL: server.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	endpoint := pool.endpoints[0]
	endpoint.mu.Lock()
	endpoint.operations[opBlock] = &operationCircuit{Failures: 1, OpenUntil: time.Now().Add(-time.Second)}
	endpoint.mu.Unlock()
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if pool.beginRead(endpoint, opBlock, time.Now()) {
				admitted.Add(1)
			}
		})
	}
	wg.Wait()
	require.Equal(t, int64(1), admitted.Load())
	pool.finishRead(endpoint, opBlock, time.Second, context.Canceled, context.Canceled)
	endpoint.mu.Lock()
	require.Equal(t, 1, endpoint.operations[opBlock].Failures)
	require.False(t, endpoint.operations[opBlock].Probing)
	endpoint.mu.Unlock()
	require.True(t, pool.beginRead(endpoint, opBlock, time.Now()))
	pool.finishRead(endpoint, opBlock, time.Millisecond, nil, nil)
	endpoint.mu.Lock()
	require.Zero(t, endpoint.operations[opBlock].Failures)
	require.True(t, endpoint.operations[opBlock].OpenUntil.IsZero())
	endpoint.mu.Unlock()
}
func TestRPCAdmissionSharedAcrossTenantPoolsAndReservesUrgentSlot(t *testing.T) {
	server := blockTestServer(t, nil)
	defer server.Close()
	cfg := RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Settings: chainconfig.EVMSettings{ProviderConcurrency: 3, BackfillConcurrency: 2}, Endpoints: []chainconfig.Endpoint{{ProviderID: "provider", URL: server.URL}}}
	first, err := NewRPCPool(t.Context(), cfg)
	require.NoError(t, err)
	defer first.Close()
	second, err := NewRPCPool(t.Context(), cfg)
	require.NoError(t, err)
	defer second.Close()
	a := first.endpoints[0].admission
	require.Same(t, a, second.endpoints[0].admission)
	leave1, err := a.Enter(t.Context(), true)
	require.NoError(t, err)
	defer leave1()
	leave2, err := a.Enter(t.Context(), true)
	require.NoError(t, err)
	defer leave2()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	leaveUrgent, err := a.Enter(ctx, false)
	require.NoError(t, err)
	leaveUrgent()
	ctx2, cancel2 := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel2()
	_, err = a.Enter(ctx2, true)
	require.Error(t, err)
}
func TestRPCAllOpenReturnsCleanUnavailableError(t *testing.T) {
	server := blockTestServer(t, nil)
	defer server.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Endpoints: []chainconfig.Endpoint{{ProviderID: "provider", URL: server.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	e := pool.endpoints[0]
	e.mu.Lock()
	e.operations[opBlock] = &operationCircuit{OpenUntil: time.Now().Add(time.Minute)}
	e.mu.Unlock()
	_, err = pool.BlockByNumber(t.Context(), big.NewInt(100))
	require.ErrorIs(t, err, ErrAllRPCProvidersUnavailable)
	require.NotContains(t, err.Error(), "%!w")
}
func TestRPCReadDoesNotExposeEndpointCredentials(t *testing.T) {
	server := blockTestServer(t, nil)
	url := server.URL + "/?api_key=secret"
	server.Close()
	// Classification deliberately discards transport strings, including URLs.
	require.Equal(t, "transport_or_decode", sanitizedRPCError(fmt.Errorf("request failed %s", url)))
}
func TestRPCResponseLimitFailsOver(t *testing.T) {
	primary := blockTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "999999")
		_, _ = fmt.Fprint(w, "{}")
	})
	defer primary.Close()
	secondary := blockTestServer(t, nil)
	defer secondary.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Settings: chainconfig.EVMSettings{MaxResponseBytes: 4096}, Endpoints: []chainconfig.Endpoint{{ProviderID: "large", URL: primary.URL}, {ProviderID: "healthy", URL: secondary.URL, Priority: 2}}})
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.BlockByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
}
func blockTestServer(t *testing.T, fullBlock http.HandlerFunc) *httptest.Server {
	t.Helper()
	header := &types.Header{ParentHash: common.HexToHash("0x01"), UncleHash: types.EmptyUncleHash, Root: types.EmptyRootHash, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: big.NewInt(100), GasLimit: 30000000, Time: uint64(time.Now().Unix()), BaseFee: big.NewInt(1)}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Method == "eth_getBlockByNumber" && len(request.Params) > 1 && string(request.Params[1]) == "true" && fullBlock != nil {
			r.Header.Set("X-Test-RPC-ID", string(request.ID))
			fullBlock(w, r)
			return
		}
		result := any(header)
		if request.Method == "eth_chainId" {
			result = "0xaa36a7"
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
}

func TestRPCRetryAfterBlocksOnlyTheFailedOperation(t *testing.T) {
	primary := blockTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer primary.Close()
	secondary := blockTestServer(t, nil)
	defer secondary.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second, Endpoints: []chainconfig.Endpoint{{ProviderID: "throttled", URL: primary.URL}, {ProviderID: "healthy", URL: secondary.URL, Priority: 2}}})
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.BlockByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	e := pool.endpoints[0]
	e.mu.Lock()
	until := e.operations[opBlock].OpenUntil
	category := e.operations[opBlock].LastError
	e.mu.Unlock()
	require.Greater(t, time.Until(until), 110*time.Second)
	require.Equal(t, "rate_limited", category)
	_, err = pool.HeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.False(t, pool.beginRead(e, opBlock, time.Now()))
}

func TestUnsupportedFeeHistoryDoesNotDisableOtherStateMethods(t *testing.T) {
	server := blockTestServer(t, nil)
	defer server.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Endpoints: []chainconfig.Endpoint{{ProviderID: "provider", URL: server.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	e := pool.endpoints[0]
	require.True(t, pool.beginRead(e, opState, time.Now()))
	pool.finishRead(e, opState, time.Millisecond, ErrRPCUnsupported, nil)
	e.mu.Lock()
	e.unsupportedMethods["state:eth_feeHistory"] = true
	e.mu.Unlock()
	require.True(t, pool.beginRead(e, opState, time.Now()), "method capability must not disable balances/nonces")
	pool.finishRead(e, opState, time.Millisecond, nil, nil)
	_, err = pool.FeeHistory(t.Context(), 1, nil)
	require.ErrorIs(t, err, ErrRPCUnsupported)
}

func TestLogsRejectingFutureRangeFailsOverInsteadOfStoppingWatcher(t *testing.T) {
	var primaryCalls, secondaryCalls atomic.Int32
	logServer := func(counter *atomic.Int32, behind bool) *httptest.Server {
		header := &types.Header{Number: big.NewInt(100), Difficulty: big.NewInt(0), GasLimit: 30000000, Time: uint64(time.Now().Unix()), BaseFee: big.NewInt(1)}
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			result := map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": header}
			if request.Method == "eth_chainId" {
				result["result"] = "0xaa36a7"
			}
			if request.Method == "eth_getLogs" {
				counter.Add(1)
				if behind {
					delete(result, "result")
					result["error"] = map[string]any{"code": -32602, "message": "block range extends beyond current head block"}
				} else {
					result["result"] = []any{}
				}
			}
			require.NoError(t, json.NewEncoder(w).Encode(result))
		}))
	}
	primary := logServer(&primaryCalls, true)
	defer primary.Close()
	secondary := logServer(&secondaryCalls, false)
	defer secondary.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Minute, Endpoints: []chainconfig.Endpoint{{ProviderID: "behind", URL: primary.URL}, {ProviderID: "current", URL: secondary.URL, Priority: 1}}})
	require.NoError(t, err)
	defer pool.Close()
	query := ethereum.FilterQuery{FromBlock: big.NewInt(99), ToBlock: big.NewInt(100)}
	_, err = pool.FilterLogs(t.Context(), query)
	require.NoError(t, err)
	_, err = pool.FilterLogs(t.Context(), query)
	require.NoError(t, err)
	require.EqualValues(t, 1, primaryCalls.Load())
	require.EqualValues(t, 2, secondaryCalls.Load(), "working logs provider remains sticky")
	e := pool.endpoints[0]
	e.mu.Lock()
	category := e.operations[opLogs].LastError
	e.mu.Unlock()
	require.Equal(t, "head_behind", category)
}

func TestUnsupportedFullTransactionsDoesNotDisableHeaderMethod(t *testing.T) {
	server := blockTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(r.Header.Get("X-Test-RPC-ID")), "error": map[string]any{"code": -32601, "message": "full transactions unavailable"}})
	})
	defer server.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Endpoints: []chainconfig.Endpoint{{ProviderID: "headers-only", URL: server.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.BlockByNumber(t.Context(), big.NewInt(100))
	require.ErrorIs(t, err, ErrRPCUnsupported)
	_, err = pool.HeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err, "the same RPC method has separate true/false capabilities")
}
func TestWrongChainCannotBeResetByAnInflightSuccess(t *testing.T) {
	endpoint := &rpcEndpoint{state: rpcStateWrongChain, lastError: "chain ID mismatch"}
	pool := &rpcGeneration{endpoints: []*rpcEndpoint{endpoint}}
	pool.success(endpoint)
	require.True(t, pool.hasWrongChainEndpoint(), "wrong chain is terminal until corrected topology/restart")
}

type archivePlanError struct{}

func (archivePlanError) ErrorCode() int { return -32002 }
func (archivePlanError) Error() string {
	return "Archive, Debug and Trace requests are not available on your current plan. Visit https://provider.invalid/private-token to upgrade.."
}
func TestArchivePlanGateDoesNotSuspendRecentBlockReads(t *testing.T) {
	require.Equal(t, "archive_unavailable", rpcErrorCategory(archivePlanError{}))
	server := blockTestServer(t, nil)
	defer server.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Endpoints: []chainconfig.Endpoint{{ProviderID: "chainstack", URL: server.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	endpoint := pool.endpoints[0]
	require.True(t, pool.beginRead(endpoint, opHeader, time.Now()))
	pool.finishRead(endpoint, opHeader, time.Millisecond, archivePlanError{}, nil)
	require.True(t, pool.beginRead(endpoint, opHeader, time.Now()), "an archive plan restriction must not open the recent-header circuit")
	pool.finishRead(endpoint, opHeader, time.Millisecond, nil, nil)
	header, err := pool.HeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, int64(100), header.Number.Int64())
	require.NotContains(t, sanitizedRPCError(archivePlanError{}), "private-token")
}
