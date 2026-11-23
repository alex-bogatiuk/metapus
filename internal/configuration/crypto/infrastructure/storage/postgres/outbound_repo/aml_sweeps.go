package outbound_repo

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

func (r *OutboundRepo) CancelSweepAttemptAllocation(ctx context.Context, sweepAttemptID id.ID, reason, actor string) error {
	return crypto_repo.NewAMLRepository().CancelSweepAttemptAllocation(ctx, sweepAttemptID, reason, actor)
}
