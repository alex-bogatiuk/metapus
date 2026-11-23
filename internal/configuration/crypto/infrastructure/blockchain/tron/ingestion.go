package tron

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/rpcpolicy"
)

func (d *Driver) ExpectedWatcherKeys(assets []crypto.AssetRef) []string {
	mode := d.rpc.settings.IngestionMode
	if mode == "hybrid" {
		return []string{"tron:solidified", "tron:latest"}
	}
	if mode == "solidified" || mode == "paused" {
		return []string{"tron:solidified"}
	}
	return []string{"tron:shadow"}
}
func (d *Driver) ReadIngestionHead(ctx context.Context, assets []crypto.AssetRef) (crypto.IngestionHead, error) {
	key := "tron:solidified"
	if d.rpc.settings.IngestionMode == "shadow" {
		key = "tron:shadow"
	}
	return d.ReadStreamHead(ctx, key, assets)
}

func (d *Driver) StreamRequired(key string) bool { return key != "tron:latest" && key != "tron:shadow" }

func (d *Driver) ReadStreamHead(ctx context.Context, key string, assets []crypto.AssetRef) (crypto.IngestionHead, error) {
	if key != "tron:latest" && key != "tron:solidified" && key != "tron:shadow" {
		return crypto.IngestionHead{}, errors.New("unknown TRON block stream")
	}
	if d.rpc.settings.IngestionMode == "paused" {
		return crypto.IngestionHead{}, crypto.ErrVerificationPending
	}
	var block *BlockInfo
	var err error
	if key == "tron:latest" {
		block, err = d.rpc.LatestHead(ctx)
	} else {
		block, err = d.rpc.QuorumHead(ctx, chainconfig.RoleSolidity)
	}
	if err != nil {
		return crypto.IngestionHead{}, err
	}
	return crypto.IngestionHead{Height: block.BlockHeader.RawData.Number, Hash: block.BlockID, Timestamp: time.UnixMilli(block.BlockHeader.RawData.Timestamp).UTC(), ObservedAt: time.Now().UTC()}, nil
}

var _ crypto.StreamIngestionReader = (*Driver)(nil)

var _ crypto.WatcherStreamDescriptor = (*Driver)(nil)
var _ crypto.ChainIngestionReader = (*Driver)(nil)

func (d *Driver) IngestionOperations() []crypto.IngestionOperationStatus {
	pool, release, err := d.rpc.runtime.Peek()
	if err != nil {
		return nil
	}
	defer release()
	var result []crypto.IngestionOperationStatus
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		for _, e := range pool.byRole[role] {
			e.mu.Lock()
			ops := make([]string, 0, len(e.operations))
			for op := range e.operations {
				ops = append(ops, op)
			}
			sort.Strings(ops)
			for _, op := range ops {
				c := e.operations[op]
				status := crypto.IngestionOperationStatus{ProviderID: e.providerID, Operation: string(role) + ":" + op, State: "closed", Failures: c.Failures, LastError: rpcpolicy.SafeReason(c.LastOutcome), LatencyMS: c.Latency.Milliseconds()}
				if status.LastError == "" {
					status.LastError = rpcpolicy.SafeReason(c.LastError)
				}
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
				result = append(result, status)
			}
			e.mu.Unlock()
		}
	}
	return result
}

var _ crypto.IngestionOperationReporter = (*Driver)(nil)
