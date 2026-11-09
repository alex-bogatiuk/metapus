package evm

import (
	"context"
	"fmt"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
	"strings"
	"time"
)

func (d *Driver) ExpectedWatcherKeys(assets []crypto.AssetRef) []string {
	keys := []string{"native_eth"}
	for _, asset := range assets {
		if asset.Standard.IsEVMContract() {
			keys = append(keys, "erc20:"+strings.ToLower(asset.ContractAddress))
		}
	}
	return keys
}
func (d *Driver) ReadIngestionHead(ctx context.Context, _ []crypto.AssetRef) (crypto.IngestionHead, error) {
	pool, release, acquireErr := d.rpc.runtime.Acquire(ctx)
	if acquireErr != nil {
		return crypto.IngestionHead{}, acquireErr
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, pool.settings.ReadBudget())
	defer cancel()
	header, err := pool.HeaderByNumber(ctx, nil)
	if err != nil {
		return crypto.IngestionHead{}, err
	}
	block, err := pool.BlockByNumber(ctx, header.Number)
	if err != nil {
		return crypto.IngestionHead{}, fmt.Errorf("full-block coverage unavailable: %w", err)
	}
	validator := Watcher{network: d.network}
	if err := validator.validateNativeBlock(block, header.Number.Int64()); err != nil {
		return crypto.IngestionHead{}, err
	}
	if block.Hash() != header.Hash() {
		return crypto.IngestionHead{}, ErrRPCProviderDivergence
	}
	return crypto.IngestionHead{Height: header.Number.Int64(), Hash: header.Hash().Hex(), Timestamp: time.Unix(int64(header.Time), 0).UTC(), ObservedAt: time.Now().UTC()}, nil
}

var _ crypto.WatcherStreamDescriptor = (*Driver)(nil)
var _ crypto.ChainIngestionReader = (*Driver)(nil)

func (d *Driver) IngestionOperations() []crypto.IngestionOperationStatus {
	pool, release, err := d.rpc.runtime.Peek()
	if err != nil {
		return nil
	}
	defer release()
	var result []crypto.IngestionOperationStatus
	for _, endpoint := range pool.endpoints {
		endpoint.mu.Lock()
		for _, op := range []rpcOperation{opHeader, opBlock, opLogs, opReceipt, opState} {
			status := crypto.IngestionOperationStatus{ProviderID: endpoint.providerID, Operation: string(op), State: "unobserved"}
			if c := endpoint.operations[op]; c != nil {
				status.State = "closed"
				status.Failures = c.Failures
				status.LastError = rpcpolicy.SafeReason(c.LastOutcome)
				if status.LastError == "" {
					status.LastError = rpcpolicy.SafeReason(c.LastError)
				}
				status.LatencyMS = c.Latency.Milliseconds()
				if !c.OpenUntil.IsZero() {
					status.State = "open"
					until := c.OpenUntil
					status.RetryAt = &until
				}
				if c.Probing {
					status.State = "half_open"
				}
				if c.Unsupported {
					status.State = "unsupported"
				}
			}
			result = append(result, status)
		}
		endpoint.mu.Unlock()
	}
	return result
}

// Close releases the read adapter owned by a standalone ingestion observer.
func (d *Driver) Close() { d.rpc.Close() }
