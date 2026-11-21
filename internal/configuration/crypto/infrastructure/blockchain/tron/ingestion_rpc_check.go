package tron

import (
	"context"
	"metapus/internal/configuration/crypto/domain/crypto"
)

// CheckIngestionRPC checks independent block/receipt coverage for each node role.
func (d *Driver) CheckIngestionRPC(ctx context.Context, _ []crypto.AssetRef, options crypto.IngestionRPCCheckOptions) (crypto.IngestionRPCCheckResult, error) {
	return d.checkBlockIngestionRPC(ctx, options)
}

var _ crypto.IngestionRPCChecker = (*Driver)(nil)
