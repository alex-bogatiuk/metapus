package evm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"metapus/internal/infrastructure/providerpolicy"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/watcherlease"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/pkg/logger"
)

var ErrDeepReorg = errors.New("EVM reorg crossed the configured lookback or finalized boundary")

type WatcherConfig struct {
	Network                crypto.NetworkRef
	RPC                    watcherRPC
	StateRepository        crypto.WatcherStateRepository
	BlockHistoryRepository crypto.WatcherBlockHistoryRepository
	NetworkHoldRecorder    crypto.NetworkOperationalHoldRecorder
	WatcherKey             string
	Asset                  *crypto.AssetRef
	Addresses              []string
	PollInterval           time.Duration
	Lookback               int64
	BlockCommitter         crypto.WatcherBlockCommitter
	Settings               chainconfig.EVMSettings
}

type watcherRPC interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*gethtypes.Header, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*gethtypes.Block, error)
	TransactionReceipt(ctx context.Context, hash [32]byte) (*gethtypes.Receipt, error)
	FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]gethtypes.Log, error)
}

type Watcher struct {
	network      crypto.NetworkRef
	rpc          watcherRPC
	stateRepo    crypto.WatcherStateRepository
	historyRepo  crypto.WatcherBlockHistoryRepository
	holdRecorder crypto.NetworkOperationalHoldRecorder
	watcherKey   string
	asset        *crypto.AssetRef
	pollInterval time.Duration
	lookback     int64
	committer    crypto.WatcherBlockCommitter
	settings     chainconfig.EVMSettings
	head         int64
	coverage     atomic.Pointer[crypto.AddressWatchCoverage]

	addressesMu sync.RWMutex
	addresses   map[common.Address]struct{}
}

func NewWatcher(cfg WatcherConfig) (*Watcher, error) {
	if err := cfg.Network.Validate(); err != nil || cfg.Network.Family != crypto.ChainFamilyEVM {
		return nil, errors.New("EVM watcher requires a valid EVM network")
	}
	if cfg.RPC == nil || cfg.StateRepository == nil || cfg.BlockHistoryRepository == nil || cfg.NetworkHoldRecorder == nil || strings.TrimSpace(cfg.WatcherKey) == "" {
		return nil, errors.New("EVM watcher requires RPC, checkpoint/history repositories, hold recorder, and key")
	}
	if cfg.BlockCommitter == nil {
		cfg.BlockCommitter, _ = cfg.StateRepository.(crypto.WatcherBlockCommitter)
	}
	if cfg.BlockCommitter == nil {
		return nil, errors.New("EVM watcher requires atomic block committer")
	}
	cfg.Settings = cfg.Settings.Defaults()
	if err := cfg.Settings.Validate(); err != nil {
		return nil, err
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 3 * time.Second
	}
	if cfg.Lookback <= 0 {
		cfg.Lookback = 64
	}
	w := &Watcher{network: cfg.Network, rpc: cfg.RPC, stateRepo: cfg.StateRepository, historyRepo: cfg.BlockHistoryRepository, holdRecorder: cfg.NetworkHoldRecorder,
		watcherKey: cfg.WatcherKey, asset: cfg.Asset, pollInterval: cfg.PollInterval, lookback: cfg.Lookback, committer: cfg.BlockCommitter, settings: cfg.Settings}
	if err := w.replaceAddresses(cfg.Addresses, true); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Watcher) Start(ctx context.Context, sink crypto.BlockchainEventSink) error {
	if sink == nil {
		return errors.New("EVM watcher event sink is required")
	}
	state, err := w.stateRepo.Get(ctx, w.network.ID, w.watcherKey)
	if errors.Is(err, crypto.ErrWatcherStateNotFound) {
		latest, headErr := w.rpc.HeaderByNumber(ctx, nil)
		if headErr != nil {
			return headErr
		}
		start := max(latest.Number.Int64()-w.lookback, 0)
		state = &crypto.WatcherState{NetworkID: w.network.ID, WatcherKey: w.watcherKey, LastBlock: start}
		if start > 0 {
			header, headerErr := w.rpc.HeaderByNumber(ctx, big.NewInt(start))
			if headerErr != nil {
				return headerErr
			}
			state.Fingerprint = header.Hash().Hex()
		}
	} else if err != nil {
		return fmt.Errorf("load EVM watcher checkpoint: %w", err)
	}
	if lease, ok := w.stateRepo.(crypto.WatcherLeaseRepository); ok {
		state, err = lease.Acquire(ctx, state, id.New().String())
		if err != nil {
			return err
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = lease.Release(releaseCtx, state)
		}()
	}
	if lease, ok := w.stateRepo.(crypto.WatcherLeaseRepository); ok {
		identity := *state
		var stop func()
		ctx, stop = watcherlease.Start(ctx, 20*time.Second, 5*time.Second, func(renewCtx context.Context) error { return lease.Renew(renewCtx, &identity) })
		defer stop()
	}
	logger.Info(ctx, "EVM watcher started", "network_id", w.network.ID, "watcher_key", w.watcherKey, "last_block", state.LastBlock)
	timer := time.NewTimer(0)
	defer timer.Stop()
	backoff := w.pollInterval
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-timer.C:
			if err := w.poll(ctx, state, sink); err != nil {
				if ctx.Err() != nil {
					return context.Cause(ctx)
				}
				if errors.Is(err, crypto.ErrWatcherLeaseLost) {
					return err
				}
				logger.Error(ctx, "EVM watcher poll failed", "network_id", w.network.ID, "watcher_key", w.watcherKey, "error", err)
				backoff = min(max(backoff, w.pollInterval)*2, 30*time.Second)
			} else {
				backoff = w.pollInterval
				if state.LastBlock < w.head {
					backoff = 0
				}
			}
			timer.Reset(backoff)
		}
	}
}

func (w *Watcher) poll(ctx context.Context, state *crypto.WatcherState, sink crypto.BlockchainEventSink) error {
	if source, ok := w.rpc.(interface {
		acquireReadWindow(context.Context) (watcherRPC, chainconfig.EVMSettings, func(), error)
	}); ok {
		rpc, settings, release, err := source.acquireReadWindow(ctx)
		if err != nil {
			return err
		}
		original := w.rpc
		w.rpc, w.settings = rpc, settings.Defaults()
		defer func() { w.rpc = original; release() }()
	}
	if err := w.renewLease(ctx, state); err != nil {
		return err
	}
	if err := w.verifyCheckpoint(ctx, state); err != nil {
		return err
	}
	latest, err := w.rpc.HeaderByNumber(ctx, nil)
	if err != nil {
		return err
	}
	w.head = latest.Number.Int64()
	tokenID := w.network.NativeTokenID
	if w.asset != nil {
		tokenID = w.asset.TokenID
	}
	w.coverage.Store(&crypto.AddressWatchCoverage{NetworkID: w.network.ID, TokenID: tokenID, WatcherKey: w.watcherKey, StartHeight: w.head + 1, LeaseOwner: state.LeaseOwner, FencingToken: state.FencingToken})
	recordWatcherLag(ctx, w.network.ID, w.watcherKey, latest.Number.Int64()-state.LastBlock)
	end := latest.Number.Int64()
	window := 100
	if w.asset == nil {
		window = w.settings.Window()
	}
	end = min(end, state.LastBlock+int64(window))
	finalizedHeight := int64(-1)
	if finalized, finalizedErr := w.rpc.HeaderByNumber(ctx, finalizedBlockNumber()); finalizedErr == nil {
		finalizedHeight = finalized.Number.Int64()
	}
	if w.asset == nil {
		return w.pollNativeWindow(ctx, state, sink, end, latest.Number.Int64(), finalizedHeight)
	}
	var tokenLogs map[int64][]gethtypes.Log
	if w.asset != nil && end > state.LastBlock {
		tokenLogs, err = w.fetchTokenLogs(ctx, state.LastBlock+1, end)
		if err != nil {
			return err
		}
	}
	for height := state.LastBlock + 1; height <= end; height++ {
		if err := w.renewLease(ctx, state); err != nil {
			return err
		}
		header, err := w.rpc.HeaderByNumber(ctx, big.NewInt(height))
		if err != nil {
			return err
		}
		confirmations := int(latest.Number.Int64() - height + 1)
		finality := finalityFor(w.network, height, confirmations, finalizedHeight)
		if err := w.processTokenBlock(ctx, state, sink, height, header, confirmations, finality, tokenLogs[height]); err != nil {
			return err
		}
		if err := w.commitBlock(ctx, state, header); err != nil {
			return err
		}
	}
	return nil
}

func (w *Watcher) renewLease(ctx context.Context, state *crypto.WatcherState) error {
	if lease, ok := w.stateRepo.(crypto.WatcherLeaseRepository); ok {
		return lease.Renew(ctx, state)
	}
	return nil
}

func (w *Watcher) verifyCheckpoint(ctx context.Context, state *crypto.WatcherState) error {
	if state.LastBlock <= 0 || state.Fingerprint == "" {
		return nil
	}
	header, err := w.checkpointHeader(ctx, big.NewInt(state.LastBlock))
	if err != nil {
		return fmt.Errorf("verify EVM checkpoint header: %w", err)
	}
	if strings.EqualFold(header.Hash().Hex(), state.Fingerprint) {
		return nil
	}
	finalized, finalizedErr := w.rpc.HeaderByNumber(ctx, finalizedBlockNumber())
	if finalizedErr == nil && state.LastBlock <= finalized.Number.Int64() {
		return w.deepReorg(ctx, state, fmt.Sprintf("checkpoint block %d was finalized", state.LastBlock))
	}
	minimum := max(state.LastBlock-w.lookback, 0)
	for height := state.LastBlock - 1; height >= minimum; height-- {
		stored, ok, historyErr := w.historyRepo.GetBlockFingerprint(ctx, w.network.ID, w.watcherKey, height)
		if historyErr != nil {
			return historyErr
		}
		if !ok {
			continue
		}
		canonical, headerErr := w.checkpointHeader(ctx, big.NewInt(height))
		if headerErr != nil {
			return headerErr
		}
		if !strings.EqualFold(stored.BlockHash, canonical.Hash().Hex()) {
			continue
		}
		previousBlock := state.LastBlock
		candidate := *state
		candidate.LastBlock, candidate.LastTimestamp, candidate.Fingerprint = height, stored.Timestamp, stored.BlockHash
		if lease, ok := w.stateRepo.(crypto.WatcherLeaseRepository); ok {
			err = lease.Rewind(ctx, &candidate, fmt.Sprintf("common ancestor %d hash %s; previous block %d", height, stored.BlockHash, previousBlock))
		} else {
			err = w.stateRepo.Save(ctx, &candidate)
		}
		if err != nil {
			return fmt.Errorf("persist EVM common ancestor checkpoint: %w", err)
		}
		if err := w.historyRepo.DeleteBlockFingerprintsAfter(ctx, w.network.ID, w.watcherKey, height); err != nil {
			return err
		}
		*state = candidate
		recordCheckpointRewind(ctx, w.network.ID, w.watcherKey, previousBlock-height, false)
		return nil
	}
	return w.deepReorg(ctx, state, fmt.Sprintf("no common ancestor within %d blocks", w.lookback))
}

func (w *Watcher) deepReorg(ctx context.Context, state *crypto.WatcherState, detail string) error {
	recordCheckpointRewind(ctx, w.network.ID, w.watcherKey, w.lookback+1, true)
	if err := w.holdRecorder.HoldNetwork(ctx, w.network.ID, crypto.OperationalHoldDeepReorg, map[string]any{
		"watcherKey": w.watcherKey, "checkpointBlock": state.LastBlock, "checkpointHash": state.Fingerprint,
		"lookback": w.lookback, "detail": detail,
	}); err != nil {
		return fmt.Errorf("%w: %s; persist network hold: %v", ErrDeepReorg, detail, err)
	}
	return fmt.Errorf("%w: %s", ErrDeepReorg, detail)
}

func (w *Watcher) processNativeBlock(ctx context.Context, state *crypto.WatcherState, sink crypto.BlockchainEventSink, height int64, block *gethtypes.Block, confirmations int, finality crypto.FinalityStatus) error {
	header := block.Header()
	for _, tx := range block.Transactions() {
		to := tx.To()
		if to == nil || tx.Value().Sign() <= 0 || !w.isMonitored(*to) {
			continue
		}
		receipt, err := w.rpc.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			return fmt.Errorf("read EVM inbound receipt %s: %w", tx.Hash(), err)
		}
		if receipt.BlockHash != block.Hash() || receipt.BlockNumber == nil || receipt.BlockNumber.Int64() != height || receipt.TxHash != tx.Hash() {
			return fmt.Errorf("EVM native receipt disagrees with block %d", height)
		}
		if receipt.Status != gethtypes.ReceiptStatusSuccessful {
			continue
		}
		from, err := gethtypes.Sender(gethtypes.LatestSignerForChainID(tx.ChainId()), tx)
		if err != nil {
			return fmt.Errorf("recover EVM inbound sender: %w", err)
		}
		amount, amountErr := types.NewRawAmountFromBigInt(tx.Value())
		if amountErr != nil {
			return amountErr
		}
		event := crypto.BlockchainEvent{NetworkID: w.network.ID, TxHash: tx.Hash().Hex(), EventIndex: 0,
			EventKind: crypto.EventKindNativeValue, FromAddress: from.Hex(), ToAddress: to.Hex(), Amount: amount,
			SourceAddress: from.Hex(), SourceNonce: evmNonce(tx.Nonce()),
			BlockNumber: height, BlockHash: header.Hash().Hex(), Finality: finality, Confirmations: confirmations,
			Canonicality:  crypto.CanonicalityCanonical,
			RequiredConfs: w.network.RequiredConfirmations, EventType: crypto.EventTypeTransfer,
			Direction: w.direction(from, *to), Timestamp: time.Unix(int64(header.Time), 0).UTC()}
		if err := w.renewLease(ctx, state); err != nil {
			return err
		}
		if err := sink.Process(ctx, event); err != nil {
			return fmt.Errorf("process EVM native event %s: %w", tx.Hash(), err)
		}
	}
	return nil
}

// fetchTokenLogs performs the two indexed ERC-20 Transfer lookups once for the
// whole checkpoint batch. Querying both indexed positions is necessary because
// JSON-RPC topic filters are ANDed across positions. Keeping the batch bounded
// by poll's 100-block window cuts the normal request count from 200 to 2 while
// retaining exact sender/receiver matching.
func (w *Watcher) fetchTokenLogs(ctx context.Context, fromHeight, toHeight int64) (map[int64][]gethtypes.Log, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	addresses := w.addressSnapshot()
	if len(addresses) == 0 {
		return map[int64][]gethtypes.Log{}, nil
	}
	contract := common.HexToAddress(w.asset.ContractAddress)
	transferTopic := erc20ABI.Events["Transfer"].ID
	addressTopics := make([]common.Hash, 0, len(addresses))
	for _, address := range addresses {
		addressTopics = append(addressTopics, common.BytesToHash(address.Bytes()))
	}
	queries := []ethereum.FilterQuery{
		{FromBlock: big.NewInt(fromHeight), ToBlock: big.NewInt(toHeight), Addresses: []common.Address{contract}, Topics: [][]common.Hash{{transferTopic}, addressTopics}},
		{FromBlock: big.NewInt(fromHeight), ToBlock: big.NewInt(toHeight), Addresses: []common.Address{contract}, Topics: [][]common.Hash{{transferTopic}, nil, addressTopics}},
	}
	seen := make(map[string]gethtypes.Log)
	for _, query := range queries {
		logs, err := w.filterLogsBounded(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("filter EVM ERC-20 logs for blocks %d-%d: %w", fromHeight, toHeight, err)
		}
		for _, log := range logs {
			height := int64(log.BlockNumber)
			if height < fromHeight || height > toHeight {
				return nil, fmt.Errorf("EVM RPC returned ERC-20 log outside requested range: block %d not in %d-%d", height, fromHeight, toHeight)
			}
			seen[fmt.Sprintf("%s:%d", log.TxHash.Hex(), log.Index)] = log
		}
	}
	byHeight := make(map[int64][]gethtypes.Log)
	for _, log := range seen {
		height := int64(log.BlockNumber)
		byHeight[height] = append(byHeight[height], log)
	}
	for height := range byHeight {
		logs := byHeight[height]
		sort.Slice(logs, func(i, j int) bool {
			if logs[i].TxIndex != logs[j].TxIndex {
				return logs[i].TxIndex < logs[j].TxIndex
			}
			return logs[i].Index < logs[j].Index
		})
		byHeight[height] = logs
	}
	return byHeight, nil
}

func (w *Watcher) processTokenBlock(ctx context.Context, state *crypto.WatcherState, sink crypto.BlockchainEventSink, height int64, header *gethtypes.Header, confirmations int, finality crypto.FinalityStatus, logs []gethtypes.Log) error {
	var transactions map[common.Hash]*gethtypes.Transaction
	contract := common.HexToAddress(w.asset.ContractAddress)
	for _, log := range logs {
		if log.Removed {
			return fmt.Errorf("EVM RPC returned removed ERC-20 log %s/%d in canonical batch", log.TxHash, log.Index)
		}
		if int64(log.BlockNumber) != height {
			return fmt.Errorf("EVM ERC-20 log %s/%d belongs to block %d, expected %d", log.TxHash, log.Index, log.BlockNumber, height)
		}
		if log.BlockHash != header.Hash() {
			return fmt.Errorf("EVM ERC-20 log %s/%d block hash changed during batch", log.TxHash, log.Index)
		}
		if len(log.Topics) != 3 || len(log.Data) != 32 {
			continue
		}
		from := common.BytesToAddress(log.Topics[1].Bytes()[12:])
		to := common.BytesToAddress(log.Topics[2].Bytes()[12:])
		if !w.isMonitored(from) && !w.isMonitored(to) {
			continue
		}
		amountBig := new(big.Int).SetBytes(log.Data)
		if amountBig.Sign() <= 0 {
			continue
		}
		receipt, err := w.rpc.TransactionReceipt(ctx, log.TxHash)
		if err != nil {
			return fmt.Errorf("read EVM ERC-20 inbound receipt %s: %w", log.TxHash, err)
		}
		if receipt.BlockHash != header.Hash() || receipt.BlockNumber == nil || receipt.BlockNumber.Int64() != height || receipt.TxHash != log.TxHash {
			return fmt.Errorf("EVM token receipt disagrees with block %d", height)
		}
		if receipt.Status != gethtypes.ReceiptStatusSuccessful {
			continue
		}
		amount, amountErr := types.NewRawAmountFromBigInt(amountBig)
		if amountErr != nil {
			return amountErr
		}
		if transactions == nil {
			block, blockErr := w.rpc.BlockByNumber(ctx, big.NewInt(height))
			if blockErr != nil {
				return fmt.Errorf("read EVM token block %d: %w", height, blockErr)
			}
			if block.Hash() != header.Hash() {
				return fmt.Errorf("EVM token body disagrees with block %d", height)
			}
			transactions = make(map[common.Hash]*gethtypes.Transaction, len(block.Transactions()))
			for _, candidate := range block.Transactions() {
				transactions[candidate.Hash()] = candidate
			}
		}
		transaction := transactions[log.TxHash]
		if transaction == nil {
			return fmt.Errorf("EVM token transaction %s is absent from canonical block", log.TxHash)
		}
		source, err := gethtypes.Sender(gethtypes.LatestSignerForChainID(transaction.ChainId()), transaction)
		if err != nil {
			return fmt.Errorf("recover EVM token transaction sender: %w", err)
		}
		event := crypto.BlockchainEvent{NetworkID: w.network.ID, TxHash: log.TxHash.Hex(), EventIndex: int64(log.Index),
			EventKind: crypto.EventKindContractLog, FromAddress: from.Hex(), ToAddress: to.Hex(), TokenContract: contract.Hex(), Amount: amount,
			SourceAddress: source.Hex(), SourceNonce: evmNonce(transaction.Nonce()),
			BlockNumber: height, BlockHash: header.Hash().Hex(), Finality: finality, Confirmations: confirmations,
			Canonicality:  crypto.CanonicalityCanonical,
			RequiredConfs: w.network.RequiredConfirmations, EventType: crypto.EventTypeTransfer,
			Direction: w.direction(from, to), Timestamp: time.Unix(int64(header.Time), 0).UTC()}
		if err := w.renewLease(ctx, state); err != nil {
			return err
		}
		if err := sink.Process(ctx, event); err != nil {
			return fmt.Errorf("process EVM ERC-20 event %s/%d: %w", log.TxHash, log.Index, err)
		}
	}
	return nil
}

func evmNonce(value uint64) *int64 {
	if value > math.MaxInt64 {
		return nil
	}
	converted := int64(value)
	return &converted
}

func (w *Watcher) ReplaceMonitoredAddresses(addresses []string) {
	_ = w.replaceAddresses(addresses, false)
}

func (w *Watcher) AddressCoverage() (crypto.AddressWatchCoverage, bool) {
	coverage := w.coverage.Load()
	if coverage == nil {
		return crypto.AddressWatchCoverage{}, false
	}
	return *coverage, coverage.StartHeight > 1 && !id.IsNil(coverage.TokenID) && coverage.LeaseOwner != "" && coverage.FencingToken > 0
}

func (w *Watcher) replaceAddresses(addresses []string, strict bool) error {
	next := make(map[common.Address]struct{}, len(addresses))
	for _, value := range addresses {
		normalized, err := NormalizeAddress(value)
		if err != nil {
			if strict {
				return fmt.Errorf("EVM watcher address %q: %w", value, err)
			}
			continue
		}
		next[common.HexToAddress(normalized)] = struct{}{}
	}
	w.addressesMu.Lock()
	w.addresses = next
	w.addressesMu.Unlock()
	return nil
}

func (w *Watcher) isMonitored(address common.Address) bool {
	w.addressesMu.RLock()
	_, ok := w.addresses[address]
	w.addressesMu.RUnlock()
	return ok
}

func (w *Watcher) addressSnapshot() []common.Address {
	w.addressesMu.RLock()
	result := make([]common.Address, 0, len(w.addresses))
	for address := range w.addresses {
		result = append(result, address)
	}
	w.addressesMu.RUnlock()
	return result
}

func (w *Watcher) direction(from, to common.Address) crypto.TransferDirection {
	fromManaged, toManaged := w.isMonitored(from), w.isMonitored(to)
	switch {
	case fromManaged && toManaged:
		return crypto.TransferDirectionInternal
	case fromManaged:
		return crypto.TransferDirectionOutgoing
	case toManaged:
		return crypto.TransferDirectionIncoming
	default:
		return crypto.TransferDirectionUnknown
	}
}

var _ crypto.ManagedWatcher = (*Watcher)(nil)

func (w *Watcher) filterLogsBounded(ctx context.Context, query ethereum.FilterQuery) ([]gethtypes.Log, error) {
	logs, err := w.rpc.FilterLogs(ctx, query)
	if err == nil {
		return logs, nil
	}
	if query.FromBlock == nil || query.ToBlock == nil || query.FromBlock.Cmp(query.ToBlock) >= 0 || (!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrRPCResponseTooLarge) && !errors.Is(err, providerpolicy.ErrQueryLimit)) {
		return nil, err
	}
	middle := new(big.Int).Add(query.FromBlock, query.ToBlock)
	middle.Div(middle, big.NewInt(2))
	left, right := query, query
	left.ToBlock = middle
	right.FromBlock = new(big.Int).Add(middle, big.NewInt(1))
	first, err := w.filterLogsBounded(ctx, left)
	if err != nil {
		return nil, err
	}
	second, err := w.filterLogsBounded(ctx, right)
	if err != nil {
		return nil, err
	}
	return append(first, second...), nil
}

func (w *Watcher) checkpointHeader(ctx context.Context, height *big.Int) (*gethtypes.Header, error) {
	if canonical, ok := w.rpc.(interface {
		CanonicalHeaderByNumber(context.Context, *big.Int) (*gethtypes.Header, error)
	}); ok {
		return canonical.CanonicalHeaderByNumber(ctx, height)
	}
	return w.rpc.HeaderByNumber(ctx, height)
}
