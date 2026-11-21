package tron

import (
	"context"
	"errors"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
)

// Hybrid watchers may initialize concurrently. Busy means the whole SQL
// attempt rolled back; retry it outside the transaction without another RPC.
func (d *Driver) ensureCutover(ctx context.Context, network crypto.NetworkRef, block crypto.WatcherBlockFingerprint, mode string, assets []crypto.AssetRef) (crypto.IngestionCutover, error) {
	for {
		if err := ctx.Err(); err != nil {
			return crypto.IngestionCutover{}, err
		}
		result, err := d.blockRepo.EnsureCutover(ctx, network, block, mode, assets)
		if !errors.Is(err, crypto.ErrIngestionRegistrationBusy) {
			return result, err
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return crypto.IngestionCutover{}, ctx.Err()
		case <-timer.C:
		}
	}
}
