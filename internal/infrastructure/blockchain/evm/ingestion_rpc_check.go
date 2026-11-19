package evm

import (
	"context"
	"errors"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"metapus/internal/domain/crypto"
)

// CheckIngestionRPC checks every endpoint directly, so a working fallback
// cannot conceal an endpoint's missing receipt/logs capability. The reference
// block comes from normal bounded routing; every successful response must
// match its height/hash. It does not mutate the ingestion circuit state.
func (d *Driver) CheckIngestionRPC(ctx context.Context, assets []crypto.AssetRef, options crypto.IngestionRPCCheckOptions) (crypto.IngestionRPCCheckResult, error) {
	result := crypto.IngestionRPCCheckResult{ObservedAt: time.Now().UTC(), NetworkCode: d.network.Code, Providers: []crypto.IngestionRPCProviderCheck{}}
	pool, release, acquireErr := d.rpc.runtime.Acquire(ctx)
	if acquireErr != nil {
		return result, acquireErr
	}
	defer release()
	if options.Samples < 1 || options.Samples > 10 || options.HistoricalBlock < 0 {
		return result, errors.New("RPC check requires 1..10 samples and a nonnegative historical block")
	}
	head, err := pool.HeaderByNumber(ctx, nil)
	if err != nil {
		return result, err
	}
	last := head.Number.Int64() - 2
	if last < int64(options.Samples) {
		return result, errors.New("RPC head has insufficient confirmed sample heights")
	}
	for i := 0; i < options.Samples; i++ {
		result.Heights = append(result.Heights, last-int64(i))
	}
	if options.HistoricalBlock > 0 {
		for i := 0; i < options.Samples; i++ {
			height := options.HistoricalBlock - int64(i)
			if height <= 0 {
				return result, errors.New("historical sample range must be positive")
			}
			if !slices.Contains(result.Heights, height) {
				result.Heights = append(result.Heights, height)
			}
		}
	}
	references := make(map[int64]*types.Block, len(result.Heights))
	for _, height := range result.Heights {
		block, err := pool.BlockByNumber(ctx, big.NewInt(height))
		if err != nil {
			return result, err
		}
		references[height] = block
	}
	addresses := make([]common.Address, 0, len(assets))
	for _, asset := range assets {
		if asset.Standard.IsEVMContract() {
			addresses = append(addresses, common.HexToAddress(asset.ContractAddress))
		}
	}
	complete := map[string]bool{}
	for _, endpoint := range pool.endpoints {
		provider := crypto.IngestionRPCProviderCheck{ProviderID: endpoint.providerID, Ready: true}
		for _, operation := range []rpcOperation{opHeader, opBlock, opReceipt, opLogs} {
			check := crypto.IngestionRPCOperationCheck{Operation: string(operation), Errors: map[string]int{}}
			durations := make([]int64, 0, len(result.Heights))
			for _, height := range result.Heights {
				if err := ctx.Err(); err != nil {
					return result, err
				}
				reference := references[height]
				if operation == opReceipt && len(reference.Transactions()) == 0 {
					continue
				}
				check.Attempted++
				started := time.Now()
				err := directIngestionRPCCheck(ctx, pool, endpoint, operation, reference, addresses)
				if err == nil {
					check.Successful++
					durations = append(durations, time.Since(started).Milliseconds())
				} else {
					check.Errors[rpcErrorCategory(err)]++
				}
				// Small reproducible checks should not contend with production
				// watchers or act as an endpoint stress test.
				timer := time.NewTimer(200 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return result, ctx.Err()
				case <-timer.C:
				}
			}
			slices.Sort(durations)
			if len(durations) > 0 {
				check.P50Millis = durations[len(durations)/2]
				check.P95Millis = durations[min(len(durations)-1, len(durations)*95/100)]
			}
			if check.Attempted == 0 || check.Attempted != check.Successful {
				provider.Ready = false
			}
			provider.Operations = append(provider.Operations, check)
		}
		if provider.Ready && endpoint.trustGroup != "" {
			complete[endpoint.trustGroup] = true
		}
		result.Providers = append(result.Providers, provider)
	}
	result.Ready = len(complete) >= 2
	return result, nil
}

func directIngestionRPCCheck(ctx context.Context, pool *rpcGeneration, endpoint *rpcEndpoint, operation rpcOperation, reference *types.Block, addresses []common.Address) error {
	endpoint.mu.Lock()
	verified := endpoint.verified && endpoint.state != rpcStateWrongChain
	endpoint.mu.Unlock()
	if !verified {
		return ErrAllRPCProvidersUnavailable
	}
	budget := pool.settings.AttemptTimeout()
	if operation == opLogs {
		budget = time.Duration(pool.settings.LogsTimeoutMS) * time.Millisecond
	}
	attempt, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	leave, err := endpoint.admission.Enter(attempt, operation == opBlock || operation == opLogs)
	if err != nil {
		return err
	}
	defer leave()
	attempt = context.WithValue(attempt, rpcOperationContextKey{}, operation)
	client := endpoint.client
	switch operation {
	case opHeader:
		header, err := client.HeaderByNumber(attempt, reference.Number())
		if err != nil {
			return err
		}
		if header.Hash() != reference.Hash() {
			return ErrRPCProviderDivergence
		}
	case opBlock:
		block, err := client.BlockByNumber(attempt, reference.Number())
		if err != nil {
			return err
		}
		validator := Watcher{network: crypto.NetworkRef{NetworkIdentifier: pool.expectedChainID.String()}}
		if err = validator.validateNativeBlock(block, reference.Number().Int64()); err != nil {
			return err
		}
		if block.Hash() != reference.Hash() {
			return ErrRPCProviderDivergence
		}
	case opReceipt:
		receipt, err := readProbeReceipt(attempt, client, reference)
		if err != nil {
			return err
		}
		if receipt.BlockNumber == nil || receipt.BlockNumber.Cmp(reference.Number()) != 0 || receipt.BlockHash != reference.Hash() || receipt.TxHash != reference.Transactions()[0].Hash() {
			return ErrRPCProviderDivergence
		}
	case opLogs:
		logs, err := client.FilterLogs(attempt, ethereum.FilterQuery{FromBlock: reference.Number(), ToBlock: reference.Number(), Addresses: addresses})
		if err != nil {
			return err
		}
		for _, log := range logs {
			if log.Removed || log.BlockNumber != reference.NumberU64() || log.BlockHash != reference.Hash() {
				return ErrRPCProviderDivergence
			}
		}
	}
	return nil
}

func readProbeReceipt(ctx context.Context, client *ethclient.Client, block *types.Block) (*types.Receipt, error) {
	return client.TransactionReceipt(ctx, block.Transactions()[0].Hash())
}
