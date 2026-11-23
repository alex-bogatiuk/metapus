package evm

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/providerpolicy"
)

func TestWatcherCheckpointWaitsForSuccessfulEventProcessing(t *testing.T) {
	network := testSepoliaNetwork()
	monitored := common.HexToAddress("0x1111111111111111111111111111111111111111")
	key, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	tx, err := gethtypes.SignNewTx(key, gethtypes.LatestSignerForChainID(big.NewInt(11155111)), &gethtypes.DynamicFeeTx{
		ChainID: big.NewInt(11155111), Nonce: 0, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
		Gas: 21000, To: &monitored, Value: big.NewInt(42),
	})
	require.NoError(t, err)
	header := &gethtypes.Header{Number: big.NewInt(1), Time: uint64(time.Now().Unix()), GasLimit: 30_000_000}
	block := gethtypes.NewBlock(header, &gethtypes.Body{Transactions: []*gethtypes.Transaction{tx}}, nil, trie.NewStackTrie(nil))
	header = block.Header()
	rpc := &fakeWatcherRPC{latest: header, finalized: &gethtypes.Header{Number: big.NewInt(0)}, headers: map[int64]*gethtypes.Header{1: header},
		blocks: map[int64]*gethtypes.Block{1: block}, receipts: map[common.Hash]*gethtypes.Receipt{
			tx.Hash(): {Status: gethtypes.ReceiptStatusSuccessful, TxHash: tx.Hash(), BlockHash: header.Hash(), BlockNumber: big.NewInt(1)},
		}}
	stateRepo := &fakeWatcherStateRepo{}
	historyRepo := &fakeWatcherHistoryRepo{}
	watcher, err := NewWatcher(WatcherConfig{Network: network, RPC: rpc, StateRepository: stateRepo,
		BlockCommitter: &fakeBlockCommitter{stateRepo: stateRepo, historyRepo: historyRepo}, BlockHistoryRepository: historyRepo, NetworkHoldRecorder: &fakeNetworkHoldRepo{}, WatcherKey: "native_eth",
		Addresses: []string{monitored.Hex()}, PollInterval: time.Second, Lookback: 64})
	require.NoError(t, err)
	state := &crypto.WatcherState{NetworkID: network.ID, WatcherKey: "native_eth"}
	_, ready := watcher.AddressCoverage()
	require.False(t, ready, "no coverage before a leased head is observed")

	sinkErr := errors.New("database commit failed")
	err = watcher.poll(t.Context(), state, crypto.BlockchainEventSinkFunc(func(context.Context, crypto.BlockchainEvent) error { return sinkErr }))
	require.ErrorIs(t, err, sinkErr)
	require.Zero(t, stateRepo.saves)
	require.Zero(t, historyRepo.saves)
	require.Zero(t, state.LastBlock)

	_, ready = watcher.AddressCoverage()
	require.False(t, ready, "an unleased replica cannot attest address coverage")
	state.LeaseOwner, state.FencingToken = "active-watcher", 7
	watcher.ReplaceMonitoredAddresses([]string{monitored.Hex()})
	var event crypto.BlockchainEvent
	err = watcher.poll(t.Context(), state, crypto.BlockchainEventSinkFunc(func(_ context.Context, candidate crypto.BlockchainEvent) error {
		event = candidate
		return nil
	}))
	require.NoError(t, err)
	require.Equal(t, 1, stateRepo.saves)
	require.Equal(t, 1, historyRepo.saves)
	require.Equal(t, int64(1), state.LastBlock)
	require.Equal(t, crypto.EventKindNativeValue, event.EventKind)
	require.Equal(t, "42", event.Amount.String())
	require.Equal(t, 1, event.Confirmations)
	require.Equal(t, crypto.CanonicalityCanonical, event.Canonicality)
	coverage, ready := watcher.AddressCoverage()
	require.True(t, ready)
	require.Equal(t, network.NativeTokenID, coverage.TokenID)
	require.Equal(t, int64(2), coverage.StartHeight, "exclude the whole prefetched head")
	require.Equal(t, "active-watcher", coverage.LeaseOwner)
	require.EqualValues(t, 7, coverage.FencingToken)
}

func TestTokenWatcherDoesNotAdvanceCheckpointWhenReceiptRPCFails(t *testing.T) {
	network := testSepoliaNetwork()
	monitored := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contract := common.HexToAddress("0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9")
	header := &gethtypes.Header{Number: big.NewInt(1), Time: uint64(time.Now().Unix()), GasLimit: 30_000_000}
	txHash := common.HexToHash("0x01")
	transfer := gethtypes.Log{Address: contract, Topics: []common.Hash{erc20ABI.Events["Transfer"].ID,
		common.BytesToHash(common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes()), common.BytesToHash(monitored.Bytes())},
		Data: common.LeftPadBytes(big.NewInt(10).Bytes(), 32), TxHash: txHash, BlockHash: header.Hash(), BlockNumber: 1, Index: 0}
	rpc := &fakeWatcherRPC{latest: header, finalized: &gethtypes.Header{Number: big.NewInt(0)}, headers: map[int64]*gethtypes.Header{1: header},
		logs: []gethtypes.Log{transfer}, receiptErr: errors.New("provider timeout")}
	stateRepo, historyRepo := &fakeWatcherStateRepo{}, &fakeWatcherHistoryRepo{}
	asset := crypto.AssetRef{TokenID: id.New(), Standard: "ERC-20", ContractAddress: contract.Hex(), Decimals: 6}
	watcher, err := NewWatcher(WatcherConfig{Network: network, RPC: rpc, StateRepository: stateRepo,
		BlockCommitter: &fakeBlockCommitter{stateRepo: stateRepo, historyRepo: historyRepo}, BlockHistoryRepository: historyRepo, NetworkHoldRecorder: &fakeNetworkHoldRepo{}, WatcherKey: "erc20:test", Asset: &asset,
		Addresses: []string{monitored.Hex()}, PollInterval: time.Second, Lookback: 64})
	require.NoError(t, err)
	state := &crypto.WatcherState{NetworkID: network.ID, WatcherKey: "erc20:test"}
	err = watcher.poll(t.Context(), state, crypto.BlockchainEventSinkFunc(func(context.Context, crypto.BlockchainEvent) error { return nil }))
	require.ErrorContains(t, err, "provider timeout")
	require.Zero(t, stateRepo.saves)
	require.Zero(t, historyRepo.saves)
}

func TestTokenWatcherFetchesOneLogBatchPerIndexedTransferPosition(t *testing.T) {
	network := testSepoliaNetwork()
	monitored := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contract := common.HexToAddress("0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9")
	headers := make(map[int64]*gethtypes.Header, 100)
	for height := int64(1); height <= 100; height++ {
		headers[height] = &gethtypes.Header{Number: big.NewInt(height), Time: uint64(height), GasLimit: 30_000_000}
	}
	rpc := &fakeWatcherRPC{
		latest:    headers[100],
		finalized: &gethtypes.Header{Number: big.NewInt(90)},
		headers:   headers,
	}
	stateRepo, historyRepo := &fakeWatcherStateRepo{}, &fakeWatcherHistoryRepo{}
	asset := crypto.AssetRef{TokenID: id.New(), Standard: "ERC-20", ContractAddress: contract.Hex(), Decimals: 6}
	watcher, err := NewWatcher(WatcherConfig{Network: network, RPC: rpc, StateRepository: stateRepo,
		BlockCommitter: &fakeBlockCommitter{stateRepo: stateRepo, historyRepo: historyRepo}, BlockHistoryRepository: historyRepo, NetworkHoldRecorder: &fakeNetworkHoldRepo{}, WatcherKey: "erc20:test", Asset: &asset,
		Addresses: []string{monitored.Hex()}, PollInterval: time.Second, Lookback: 64})
	require.NoError(t, err)
	state := &crypto.WatcherState{NetworkID: network.ID, WatcherKey: "erc20:test"}

	require.NoError(t, watcher.poll(t.Context(), state, crypto.BlockchainEventSinkFunc(func(context.Context, crypto.BlockchainEvent) error {
		return nil
	})))
	require.Len(t, rpc.filterQueries, 2)
	for _, query := range rpc.filterQueries {
		require.EqualValues(t, 1, query.FromBlock.Int64())
		require.EqualValues(t, 100, query.ToBlock.Int64())
	}
	require.EqualValues(t, 100, state.LastBlock)
	require.Equal(t, 100, stateRepo.saves)
	require.Equal(t, 100, historyRepo.saves)
}

func testSepoliaNetwork() crypto.NetworkRef {
	return crypto.NetworkRef{ID: id.New(), Code: "ETHEREUM-SEPOLIA", Family: crypto.ChainFamilyEVM,
		NetworkIdentifier: "11155111", NativeTokenID: id.New(), NativeDecimals: 18,
		RequiredConfirmations: 12, FinalityMode: crypto.FinalityByConfirmationsOrHead}
}

type fakeWatcherRPC struct {
	latest, finalized *gethtypes.Header
	headers           map[int64]*gethtypes.Header
	blocks            map[int64]*gethtypes.Block
	receipts          map[common.Hash]*gethtypes.Receipt
	logs              []gethtypes.Log
	receiptErr        error
	filterQueries     []ethereum.FilterQuery
}

func (f *fakeWatcherRPC) HeaderByNumber(_ context.Context, number *big.Int) (*gethtypes.Header, error) {
	if number == nil {
		return f.latest, nil
	}
	if number.Sign() < 0 {
		return f.finalized, nil
	}
	header, ok := f.headers[number.Int64()]
	if !ok {
		return nil, ethereum.NotFound
	}
	return header, nil
}

func (f *fakeWatcherRPC) BlockByNumber(_ context.Context, number *big.Int) (*gethtypes.Block, error) {
	block, ok := f.blocks[number.Int64()]
	if !ok {
		return nil, ethereum.NotFound
	}
	return block, nil
}

func (f *fakeWatcherRPC) TransactionReceipt(_ context.Context, hash [32]byte) (*gethtypes.Receipt, error) {
	if f.receiptErr != nil {
		return nil, f.receiptErr
	}
	receipt, ok := f.receipts[common.Hash(hash)]
	if !ok {
		return nil, ethereum.NotFound
	}
	return receipt, nil
}

func (f *fakeWatcherRPC) FilterLogs(_ context.Context, query ethereum.FilterQuery) ([]gethtypes.Log, error) {
	f.filterQueries = append(f.filterQueries, query)
	return append([]gethtypes.Log(nil), f.logs...), nil
}

type fakeWatcherStateRepo struct{ saves int }

func (*fakeWatcherStateRepo) Get(context.Context, id.ID, string) (*crypto.WatcherState, error) {
	return nil, crypto.ErrWatcherStateNotFound
}
func (r *fakeWatcherStateRepo) Save(context.Context, *crypto.WatcherState) error {
	r.saves++
	return nil
}

type fakeWatcherHistoryRepo struct{ saves int }

func (*fakeWatcherHistoryRepo) GetBlockFingerprint(context.Context, id.ID, string, int64) (crypto.WatcherBlockFingerprint, bool, error) {
	return crypto.WatcherBlockFingerprint{}, false, nil
}
func (r *fakeWatcherHistoryRepo) SaveBlockFingerprint(context.Context, id.ID, string, crypto.WatcherBlockFingerprint, int64) error {
	r.saves++
	return nil
}
func (*fakeWatcherHistoryRepo) DeleteBlockFingerprintsAfter(context.Context, id.ID, string, int64) error {
	return nil
}

type fakeNetworkHoldRepo struct{ holds int }

func (r *fakeNetworkHoldRepo) HoldNetwork(context.Context, id.ID, crypto.OperationalHoldReason, map[string]any) error {
	r.holds++
	return nil
}

type fakeBlockCommitter struct {
	stateRepo   *fakeWatcherStateRepo
	historyRepo *fakeWatcherHistoryRepo
	err         error
	heights     []int64
}

func (f *fakeBlockCommitter) CommitBlock(_ context.Context, commit crypto.WatcherBlockCommit) error {
	if f.err != nil {
		return f.err
	}
	if commit.Candidate.LastBlock != commit.ExpectedBlock+1 {
		return crypto.ErrWatcherCheckpointConflict
	}
	f.stateRepo.saves++
	f.historyRepo.saves++
	f.heights = append(f.heights, commit.Candidate.LastBlock)
	return nil
}

type rangeLimitedWatcherRPC struct {
	*fakeWatcherRPC
	fail bool
}

func (r *rangeLimitedWatcherRPC) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]gethtypes.Log, error) {
	r.filterQueries = append(r.filterQueries, q)
	if q.ToBlock.Int64()-q.FromBlock.Int64()+1 > 4 {
		return nil, &RPCReadFailure{Category: "query_limit"}
	}
	if r.fail && q.FromBlock.Int64() > 4 {
		return nil, errors.New("unavailable subrange")
	}
	var logs []gethtypes.Log
	for block := q.FromBlock.Int64(); block <= q.ToBlock.Int64(); block++ {
		logs = append(logs, gethtypes.Log{BlockNumber: uint64(block)})
	}
	return logs, nil
}
func TestWatcherSplitsConfirmedQueryLimitWithoutLosingCoverage(t *testing.T) {
	for _, fail := range []bool{false, true} {
		rpc := &rangeLimitedWatcherRPC{fakeWatcherRPC: &fakeWatcherRPC{}, fail: fail}
		w := &Watcher{rpc: rpc}
		logs, err := w.filterLogsBounded(t.Context(), ethereum.FilterQuery{FromBlock: big.NewInt(1), ToBlock: big.NewInt(10)})
		if fail {
			require.Error(t, err)
			require.Nil(t, logs)
			continue
		}
		require.NoError(t, err)
		require.Len(t, logs, 10)
		for i, log := range logs {
			require.EqualValues(t, i+1, log.BlockNumber)
		}
	}
	for category, target := range map[string]error{"query_limit": providerpolicy.ErrQueryLimit, "credentials_expired": providerpolicy.ErrCredentialsExpired} {
		err := &RPCReadFailure{Category: category}
		require.ErrorIs(t, err, target)
		require.Equal(t, category, rpcErrorCategory(err))
	}
}
