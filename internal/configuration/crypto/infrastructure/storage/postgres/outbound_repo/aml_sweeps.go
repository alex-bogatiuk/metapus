package outbound_repo

import (
	"context"

	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/id"
)

func (r *OutboundRepo) CancelSweepAttemptAllocation(ctx context.Context, sweepAttemptID id.ID, reason, actor string) error {
	return crypto_repo.NewAMLRepository().CancelSweepAttemptAllocation(ctx, sweepAttemptID, reason, actor)
}
