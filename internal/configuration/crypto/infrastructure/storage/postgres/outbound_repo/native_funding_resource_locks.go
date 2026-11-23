package outbound_repo

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// Operational updates fire the same resource queue hooks as settlement. Keep
// their whole scope/wallet prefix in the transaction owning the funding row.
func mutateNativeFunding(ctx context.Context, funding id.ID, mutate func(context.Context) error) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := crypto_repo.LockNativeFundingResources(ctx, funding); err != nil {
			return err
		}
		return mutate(ctx)
	})
}
