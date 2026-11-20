package evm

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
	"metapus/internal/infrastructure/providerpolicy"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type laneTestRPC struct {
	*controlledFetchRPC
	lanes      func() []blockLane
	laneCalls  atomic.Int64
	mu         sync.Mutex
	ordinaryAt []int64
}

func (r *laneTestRPC) blockLanes() []blockLane {
	r.laneCalls.Add(1)
	return r.lanes()
}

func (r *laneTestRPC) BlockByNumber(ctx context.Context, n *big.Int) (*types.Block, error) {
	r.mu.Lock()
	r.ordinaryAt = append(r.ordinaryAt, n.Int64())
	r.mu.Unlock()
	return r.controlledFetchRPC.BlockByNumber(ctx, n)
}

func newLaneTestWatcher(t *testing.T, rpc watcherRPC, committer *fakeBlockCommitter) *Watcher {
	t.Helper()
	w, err := NewWatcher(WatcherConfig{Network: testSepoliaNetwork(), RPC: rpc, StateRepository: committer.stateRepo, BlockHistoryRepository: committer.historyRepo, BlockCommitter: committer, NetworkHoldRecorder: &fakeNetworkHoldRepo{}, WatcherKey: "native_eth", Settings: chainconfig.EVMSettings{NativeConcurrency: 2, PrefetchBlocks: 16}})
	require.NoError(t, err)
	return w
}

func TestNativeLanesServeOnlyDeepHeightsAndCommitInOrder(t *testing.T) {
	blocks, headers := linkedBlocks(12)
	laneServed := make(chan struct{})
	var once sync.Once
	var laneMu sync.Mutex
	var laneHeights []int64
	var outcome blockLaneOutcome
	rpc := &laneTestRPC{controlledFetchRPC: &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[12], finalized: headers[1], headers: headers}}}
	rpc.fetch = func(ctx context.Context, height int64) (*types.Block, error) {
		if height <= 12-nativeLaneTipDistance {
			select { // Leave deep work for the lane until it has served once.
			case <-laneServed:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return blocks[height], nil
	}
	rpc.lanes = func() []blockLane {
		return []blockLane{{provider: "second", width: 2, read: func(_ context.Context, height int64) (*types.Block, error) {
			laneMu.Lock()
			laneHeights = append(laneHeights, height)
			laneMu.Unlock()
			once.Do(func() { close(laneServed) })
			return blocks[height], nil
		}, finish: func(o blockLaneOutcome) { outcome = o }}}
	}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newLaneTestWatcher(t, rpc, c)
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.NoError(t, w.poll(t.Context(), state, noopSink()))

	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, c.heights)
	require.NotEmpty(t, laneHeights)
	for _, height := range laneHeights {
		require.LessOrEqual(t, height, int64(12-nativeLaneTipDistance), "tip heights stay on the ordinary path")
	}
	require.Len(t, append(append([]int64{}, laneHeights...), rpc.ordinaryAt...), 12, "every height is fetched exactly once")
	require.Equal(t, len(laneHeights), outcome.Served)
	require.False(t, outcome.Busy)
	require.False(t, outcome.Failed)
	require.Zero(t, rpc.active.Load())
}

func TestNativeLaneFailureFallsBackAndClosesLane(t *testing.T) {
	blocks, headers := linkedBlocks(12)
	var laneReads atomic.Int64
	var outcome blockLaneOutcome
	rpc := &laneTestRPC{controlledFetchRPC: &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[12], finalized: headers[1], headers: headers}}}
	rpc.fetch = func(_ context.Context, height int64) (*types.Block, error) { return blocks[height], nil }
	rpc.lanes = func() []blockLane {
		return []blockLane{{provider: "down", width: 2, read: func(context.Context, int64) (*types.Block, error) {
			laneReads.Add(1)
			return nil, errors.New("provider down")
		}, finish: func(o blockLaneOutcome) { outcome = o }}}
	}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newLaneTestWatcher(t, rpc, c)
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.NoError(t, w.poll(t.Context(), state, noopSink()))

	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, c.heights)
	require.LessOrEqual(t, laneReads.Load(), int64(2), "a failed lane takes no further heights")
	require.Len(t, rpc.ordinaryAt, 12, "every height the lane could not serve is read through failover")
	require.True(t, outcome.Failed)
	require.Zero(t, outcome.Served)
}

func TestNativeLaneAdmissionPressureRetiresOnlyThatWorker(t *testing.T) {
	blocks, headers := linkedBlocks(12)
	var first atomic.Bool
	var outcome blockLaneOutcome
	rpc := &laneTestRPC{controlledFetchRPC: &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[12], finalized: headers[1], headers: headers}}}
	rpc.fetch = func(_ context.Context, height int64) (*types.Block, error) { return blocks[height], nil }
	rpc.lanes = func() []blockLane {
		return []blockLane{{provider: "paced", width: 2, read: func(_ context.Context, height int64) (*types.Block, error) {
			if first.CompareAndSwap(false, true) {
				return nil, errors.Join(ErrRPCAdmissionBusy, context.DeadlineExceeded)
			}
			return blocks[height], nil
		}, finish: func(o blockLaneOutcome) { outcome = o }}}
	}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newLaneTestWatcher(t, rpc, c)
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.NoError(t, w.poll(t.Context(), state, noopSink()))

	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, c.heights)
	require.True(t, outcome.Busy)
	require.False(t, outcome.Failed)
	require.Equal(t, 1, outcome.Survivors)
}

func TestNativeLanesAreNotRequestedNearHead(t *testing.T) {
	blocks, headers := linkedBlocks(3)
	rpc := &laneTestRPC{controlledFetchRPC: &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[3], finalized: headers[1], headers: headers}}}
	rpc.fetch = func(_ context.Context, height int64) (*types.Block, error) { return blocks[height], nil }
	rpc.lanes = func() []blockLane { t.Fatal("lanes requested for a tip-only window"); return nil }
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newLaneTestWatcher(t, rpc, c)
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.NoError(t, w.poll(t.Context(), state, noopSink()))
	require.Equal(t, []int64{1, 2, 3}, c.heights)
	require.Zero(t, rpc.laneCalls.Load())
}

func laneEndpoint(provider, group string, priority int) *rpcEndpoint {
	return &rpcEndpoint{providerID: provider, trustGroup: group, priority: priority, verified: true, state: rpcStateClosed,
		operations: map[rpcOperation]*operationCircuit{}, unsupportedMethods: map[string]bool{}}
}

func laneProviders(lanes []blockLane) []string {
	providers := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		providers = append(providers, lane.provider)
	}
	return providers
}

func TestBlockLanesUseOneHealthyLanePerProviderBesidesLeader(t *testing.T) {
	leader := laneEndpoint("alchemy-a", "alchemy", 0)
	sameProvider := laneEndpoint("alchemy-b", "alchemy", 1)
	infuraA := laneEndpoint("infura-a", "infura", 2)
	infuraB := laneEndpoint("infura-b", "infura", 3)
	quota := laneEndpoint("onfinality", "onfinality", 4)
	quota.quotaLimited = true
	open := laneEndpoint("ankr", "ankr", 5)
	open.operations[opBlock] = &rpcpolicy.Circuit{OpenUntil: time.Now().Add(time.Minute)}
	unverified := laneEndpoint("drpc", "drpc", 6)
	unverified.verified = false
	gateway := laneEndpoint("gateway", "", 7)
	p := &rpcGeneration{endpoints: []*rpcEndpoint{leader, sameProvider, infuraA, infuraB, quota, open, unverified, gateway},
		settings: chainconfig.EVMSettings{NativeConcurrency: 4}.Defaults(), preferred: map[rpcOperation]*rpcEndpoint{}}

	require.Equal(t, []string{"infura-a", "gateway"}, laneProviders(p.blockLanes()))

	p.preferred[opBlock] = infuraB
	require.Equal(t, []string{"alchemy-a", "gateway"}, laneProviders(p.blockLanes()), "the sticky leader's provider gets no extra lane")
}

func TestBlockLaneWidthStartsNarrowAndAdaptsToPacing(t *testing.T) {
	endpoint := laneEndpoint("infura", "infura", 1)
	p := &rpcGeneration{endpoints: []*rpcEndpoint{laneEndpoint("alchemy", "alchemy", 0), endpoint},
		settings: chainconfig.EVMSettings{NativeConcurrency: 3}.Defaults(), preferred: map[rpcOperation]*rpcEndpoint{}}
	width := func() int { return p.blockLanes()[0].width }

	require.Equal(t, 1, width())
	p.blockLanes()[0].finish(blockLaneOutcome{Served: 0, Survivors: 1})
	require.Equal(t, 1, width(), "an idle lane does not widen")
	for range 5 {
		lane := p.blockLanes()[0]
		lane.finish(blockLaneOutcome{Served: lane.width, Survivors: lane.width})
	}
	require.Equal(t, 3, width(), "a saturated lane widens up to NativeConcurrency")
	p.blockLanes()[0].finish(blockLaneOutcome{Served: 5, Survivors: 2, Busy: true})
	require.Equal(t, 2, width(), "admission pressure keeps only workers that kept up")
	p.blockLanes()[0].finish(blockLaneOutcome{Served: 2, Survivors: 2, Failed: true})
	require.Equal(t, 2, width(), "a failed window does not widen")
}

// freshLinkedBlocks returns a chain whose head passes the stale-head check.
func freshLinkedBlocks(count int) map[int64]*types.Block {
	blocks := make(map[int64]*types.Block)
	base := uint64(time.Now().Unix()) - uint64(count) - 5
	var parent common.Hash
	for height := int64(1); height <= int64(count); height++ {
		header := &types.Header{Number: big.NewInt(height), Time: base + uint64(height), ParentHash: parent, Difficulty: big.NewInt(0), GasLimit: 30000000, Root: types.EmptyRootHash, BaseFee: big.NewInt(1)}
		blocks[height] = types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
		parent = blocks[height].Hash()
	}
	return blocks
}

func newChainRPCServer(t *testing.T, blocks map[int64]*types.Block, head int64, delay time.Duration, served func(int64)) *httptest.Server {
	t.Helper()
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
		var result any
		switch request.Method {
		case "eth_chainId":
			result = "0xaa36a7"
		case "eth_getBlockByNumber":
			tag := strings.Trim(string(request.Params[0]), `"`)
			height := head
			switch tag {
			case "latest":
			case "finalized", "safe":
				height = 1
			default:
				parsed, err := strconv.ParseInt(strings.TrimPrefix(tag, "0x"), 16, 64)
				if err != nil {
					t.Error(err)
					return
				}
				height = parsed
			}
			block := blocks[height]
			if string(request.Params[1]) != "true" {
				result = block.Header()
				break
			}
			time.Sleep(delay)
			served(height)
			encoded, err := json.Marshal(block.Header())
			require.NoError(t, err)
			var object map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &object))
			object["transactions"], object["uncles"] = json.RawMessage(`[]`), json.RawMessage(`[]`)
			result = object
		default:
			t.Errorf("unexpected method %s", request.Method)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
}

func TestRPCPoolSpreadsCatchUpBlocksOverProvidersWithoutDuplicates(t *testing.T) {
	const head = 12
	blocks := freshLinkedBlocks(head)
	var mu sync.Mutex
	servedBy := map[string][]int64{}
	record := func(provider string) func(int64) {
		return func(height int64) { mu.Lock(); servedBy[provider] = append(servedBy[provider], height); mu.Unlock() }
	}
	leader := newChainRPCServer(t, blocks, head, 20*time.Millisecond, record("leader"))
	defer leader.Close()
	second := newChainRPCServer(t, blocks, head, 0, record("second"))
	defer second.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "ETHEREUM-SEPOLIA", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second,
		Settings:  chainconfig.EVMSettings{NativeConcurrency: 2, PrefetchBlocks: 16},
		Endpoints: []chainconfig.Endpoint{{ProviderID: "leader", TrustGroup: "a", URL: leader.URL}, {ProviderID: "second", TrustGroup: "b", Priority: 1, URL: second.URL}}})
	require.NoError(t, err)
	defer pool.Close()
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newLaneTestWatcher(t, pool, c)
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.NoError(t, w.poll(t.Context(), state, noopSink()))

	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, c.heights)
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, servedBy["second"], "an idle second provider takes part of the backlog")
	for _, height := range servedBy["second"] {
		require.LessOrEqual(t, height, int64(head-nativeLaneTipDistance))
	}
	require.Len(t, append(append([]int64{}, servedBy["leader"]...), servedBy["second"]...), head, "no block is requested twice")
}

func TestEVMReadsMarkOnlyBulkOperationsForPoolAdmission(t *testing.T) {
	blocks := freshLinkedBlocks(3)
	server := newChainRPCServer(t, blocks, 3, 0, func(int64) {})
	defer server.Close()
	var mu sync.Mutex
	marked := map[string]bool{}
	record := func(base http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if op, ok := r.Context().Value(rpcOperationContextKey{}).(rpcOperation); ok {
				mu.Lock()
				marked[string(op)] = providerpolicy.IsBulk(r.Context())
				mu.Unlock()
			}
			return base.RoundTrip(r)
		})
	}
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "ETHEREUM-SEPOLIA", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second,
		Endpoints: []chainconfig.Endpoint{{ProviderID: "only", TrustGroup: "a", URL: server.URL, WrapTransport: record}}})
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.BlockByNumber(t.Context(), big.NewInt(2))
	require.NoError(t, err)
	_, err = pool.HeaderByNumber(t.Context(), big.NewInt(2))
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.True(t, marked[string(opBlock)], "full blocks are bulk")
	require.False(t, marked[string(opHeader)], "headers stay urgent")
}
