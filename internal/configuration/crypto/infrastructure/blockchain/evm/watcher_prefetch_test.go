package evm

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/id"
)

type controlledFetchRPC struct {
	*fakeWatcherRPC
	fetch             func(context.Context, int64) (*types.Block, error)
	historicalHeaders atomic.Int64
	active            atomic.Int64
	maximum           atomic.Int64
}

func (r *controlledFetchRPC) BlockByNumber(ctx context.Context, n *big.Int) (*types.Block, error) {
	active := r.active.Add(1)
	defer r.active.Add(-1)
	for {
		old := r.maximum.Load()
		if active <= old || r.maximum.CompareAndSwap(old, active) {
			break
		}
	}
	return r.fetch(ctx, n.Int64())
}
func (r *controlledFetchRPC) HeaderByNumber(ctx context.Context, n *big.Int) (*types.Header, error) {
	if n != nil && n.Sign() >= 0 {
		r.historicalHeaders.Add(1)
	}
	return r.fakeWatcherRPC.HeaderByNumber(ctx, n)
}
func linkedBlocks(count int) (map[int64]*types.Block, map[int64]*types.Header) {
	blocks := make(map[int64]*types.Block)
	headers := make(map[int64]*types.Header)
	var parent common.Hash
	for height := int64(1); height <= int64(count); height++ {
		header := &types.Header{Number: big.NewInt(height), Time: uint64(height), ParentHash: parent, Difficulty: big.NewInt(0), GasLimit: 30000000}
		block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
		blocks[height] = block
		headers[height] = block.Header()
		parent = block.Hash()
	}
	return blocks, headers
}
func newPrefetchTestWatcher(t *testing.T, rpc watcherRPC, committer *fakeBlockCommitter) *Watcher {
	t.Helper()
	w, err := NewWatcher(WatcherConfig{Network: testSepoliaNetwork(), RPC: rpc, StateRepository: committer.stateRepo, BlockHistoryRepository: committer.historyRepo, BlockCommitter: committer, NetworkHoldRecorder: &fakeNetworkHoldRepo{}, WatcherKey: "native_eth", Settings: chainconfig.EVMSettings{NativeConcurrency: 3, PrefetchBlocks: 3}})
	require.NoError(t, err)
	return w
}
func TestNativePrefetchConsumesOutOfOrderResultsInHeightOrder(t *testing.T) {
	blocks, headers := linkedBlocks(3)
	later := make(chan struct{})
	var once sync.Once
	rpc := &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[3], finalized: headers[1], headers: headers}}
	rpc.fetch = func(ctx context.Context, height int64) (*types.Block, error) {
		if height == 1 {
			select {
			case <-later:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			once.Do(func() { close(later) })
		}
		return blocks[height], nil
	}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newPrefetchTestWatcher(t, rpc, c)
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.NoError(t, w.poll(t.Context(), state, noopSink()))
	require.Equal(t, []int64{1, 2, 3}, c.heights)
	require.Equal(t, int64(3), state.LastBlock)
	require.Zero(t, rpc.historicalHeaders.Load(), "native body supplies its header")
	require.LessOrEqual(t, rpc.maximum.Load(), int64(3))
	require.Zero(t, rpc.active.Load())
}
func TestNativePrefetchGapRetriesWithoutSkippingOrLeakingWorkers(t *testing.T) {
	blocks, headers := linkedBlocks(3)
	var fail atomic.Bool
	fail.Store(true)
	rpc := &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[3], finalized: headers[1], headers: headers}}
	rpc.fetch = func(_ context.Context, height int64) (*types.Block, error) {
		if height == 2 && fail.Load() {
			return nil, errors.New("full body timeout")
		}
		return blocks[height], nil
	}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newPrefetchTestWatcher(t, rpc, c)
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.ErrorContains(t, w.poll(t.Context(), state, noopSink()), "timeout")
	require.Equal(t, []int64{1}, c.heights)
	require.Zero(t, rpc.active.Load())
	fail.Store(false)
	require.NoError(t, w.poll(t.Context(), state, noopSink()))
	require.Equal(t, []int64{1, 2, 3}, c.heights)
}
func TestNativePrefetchCancellationWaitsForFetchWorkers(t *testing.T) {
	_, headers := linkedBlocks(3)
	started := make(chan struct{})
	var once sync.Once
	rpc := &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[3], finalized: headers[1], headers: headers}}
	rpc.fetch = func(ctx context.Context, _ int64) (*types.Block, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newPrefetchTestWatcher(t, rpc, c)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- w.poll(ctx, &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}, noopSink())
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Zero(t, rpc.active.Load())
	require.Empty(t, c.heights)
}
func TestNativePrefetchParentMismatchAndFailedCommitKeepCheckpoint(t *testing.T) {
	blocks, headers := linkedBlocks(2)
	rpc := &fakeWatcherRPC{latest: headers[2], finalized: headers[1], headers: headers, blocks: blocks}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}, err: errors.New("history transaction rollback")}
	w := newPrefetchTestWatcher(t, rpc, c)
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.ErrorContains(t, w.poll(t.Context(), state, noopSink()), "rollback")
	require.Zero(t, state.LastBlock)
	c.err = nil
	state.LastBlock = 1
	state.Fingerprint = common.HexToHash("0xff").Hex()
	require.ErrorContains(t, w.pollNativeWindow(t.Context(), state, noopSink(), 2, 2, 1), "parent")
	require.Equal(t, int64(1), state.LastBlock)
	require.Empty(t, c.heights)
}
func TestCheckpointRPCFailureDoesNotCreateDeepReorgHold(t *testing.T) {
	hold := &fakeNetworkHoldRepo{}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newPrefetchTestWatcher(t, &fakeWatcherRPC{headers: map[int64]*types.Header{}, finalized: &types.Header{Number: big.NewInt(100)}}, c)
	w.holdRecorder = hold
	state := &crypto.WatcherState{NetworkID: id.New(), WatcherKey: w.watcherKey, LastBlock: 50, Fingerprint: "0x01"}
	require.ErrorContains(t, w.verifyCheckpoint(t.Context(), state), "checkpoint header")
	require.Zero(t, hold.holds)
	require.Equal(t, int64(50), state.LastBlock)
}
func noopSink() crypto.BlockchainEventSink {
	return crypto.BlockchainEventSinkFunc(func(context.Context, crypto.BlockchainEvent) error { return nil })
}

func TestPrefetchedNativePaymentUsesRefreshedMonitoredAddressesAndRejectsForeignReceipt(t *testing.T) {
	blocks, headers := linkedBlocks(2)
	monitored := common.HexToAddress("0x1111111111111111111111111111111111111111")
	key, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	tx, err := types.SignNewTx(key, types.LatestSignerForChainID(big.NewInt(11155111)), &types.DynamicFeeTx{ChainID: big.NewInt(11155111), GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), Gas: 21000, To: &monitored, Value: big.NewInt(42)})
	require.NoError(t, err)
	blocks[2] = types.NewBlock(headers[2], &types.Body{Transactions: []*types.Transaction{tx}}, nil, trie.NewStackTrie(nil))
	headers[2] = blocks[2].Header()
	rpc := &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[2], finalized: headers[1], headers: headers, receipts: map[common.Hash]*types.Receipt{tx.Hash(): {TxHash: tx.Hash(), BlockHash: blocks[2].Hash(), BlockNumber: big.NewInt(2), Status: types.ReceiptStatusSuccessful}}}}
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newPrefetchTestWatcher(t, rpc, c)
	refreshed := make(chan struct{})
	rpc.fetch = func(ctx context.Context, height int64) (*types.Block, error) {
		if height == 1 {
			select {
			case <-refreshed:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			w.ReplaceMonitoredAddresses([]string{monitored.Hex()})
			close(refreshed)
		}
		return blocks[height], nil
	}
	var events []crypto.BlockchainEvent
	state := &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey}
	require.NoError(t, w.poll(t.Context(), state, crypto.BlockchainEventSinkFunc(func(_ context.Context, event crypto.BlockchainEvent) error {
		events = append(events, event)
		return nil
	})))
	require.Len(t, events, 1)
	require.Equal(t, tx.Hash().Hex(), events[0].TxHash)
	// A receipt from another fork is unknown evidence, never a successful skip.
	rpc.receipts[tx.Hash()].BlockHash = common.HexToHash("0xff")
	state.LastBlock = 1
	state.Fingerprint = blocks[1].Hash().Hex()
	c.heights = nil
	rpc.fetch = func(_ context.Context, height int64) (*types.Block, error) { return blocks[height], nil }
	require.ErrorContains(t, w.poll(t.Context(), state, noopSink()), "receipt disagrees")
	require.Equal(t, int64(1), state.LastBlock)
	require.Empty(t, c.heights)
}

func TestWatcherBacklogErrorRestoresNonzeroRetryDelay(t *testing.T) {
	blocks, headers := linkedBlocks(2)
	rpc := &controlledFetchRPC{fakeWatcherRPC: &fakeWatcherRPC{latest: headers[2], finalized: headers[1], headers: map[int64]*types.Header{}}}
	rpc.fetch = func(_ context.Context, height int64) (*types.Block, error) { return blocks[height], nil }
	c := &fakeBlockCommitter{stateRepo: &fakeWatcherStateRepo{}, historyRepo: &fakeWatcherHistoryRepo{}}
	w := newPrefetchTestWatcher(t, rpc, c)
	w.settings.NativeConcurrency = 1
	w.settings.PrefetchBlocks = 1
	w.pollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, w.Start(ctx, noopSink()), context.DeadlineExceeded)
	require.Equal(t, []int64{1}, c.heights)
	require.LessOrEqual(t, rpc.historicalHeaders.Load(), int64(5), "backlog's immediate success polling must not create a retry storm after RPC failure")
}
