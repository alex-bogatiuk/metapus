package outbound_repo

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

func (r *OutboundRepo) CancelSweepAttemptAllocation(ctx context.Context, sweepAttemptID id.ID, reason, actor string) error {
	return crypto_repo.NewAMLRepository().CancelSweepAttemptAllocation(ctx, sweepAttemptID, reason, actor)
}
