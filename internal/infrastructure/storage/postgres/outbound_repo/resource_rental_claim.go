package outbound_repo

import (
	"context"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
)

// Validate only after account -> rental locks, using wall time after any wait.
// An unclaimed recovery command must not invalidate a live maintenance owner.
func validateResourceRentalClaim(ctx context.Context, rental id.ID, claim *outboundapp.ResourceRentalClaim) error {
	var live bool
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if claim == nil {
		if err := q.QueryRow(ctx, `SELECT COALESCE(lease_expires_at>clock_timestamp(),FALSE) FROM doc_crypto_resource_rentals WHERE id=$1`, rental).Scan(&live); err != nil {
			return err
		}
		if live {
			return apperror.NewConflict("resource rental is owned by a maintenance claim")
		}
		return nil
	}
	if claim.RentalID != rental || id.IsNil(claim.Token) {
		return apperror.NewConflict("resource rental maintenance claim is missing or stale")
	}
	if err := q.QueryRow(ctx, `SELECT COALESCE(maintenance_claim_token=$2 AND lease_expires_at>clock_timestamp(),FALSE) FROM doc_crypto_resource_rentals WHERE id=$1`, rental, claim.Token).Scan(&live); err != nil {
		return err
	}
	if !live {
		return apperror.NewConflict("resource rental maintenance claim is missing or stale")
	}
	return nil
}

func mutateResourceRentalClaim(ctx context.Context, rental id.ID, claim *outboundapp.ResourceRentalClaim, mutate func(context.Context) error) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockRentalFinancialResources(ctx, rental); err != nil {
			return err
		}
		if err := validateResourceRentalClaim(ctx, rental, claim); err != nil {
			return err
		}
		return mutate(ctx)
	})
}

func clearResourceRentalClaim(ctx context.Context, rental id.ID) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_resource_rentals SET maintenance_claim_token=NULL,claimed_by='',claimed_at=NULL,lease_expires_at=NULL,updated_at=NOW() WHERE id=$1`, rental)
	return err
}

func (r *OutboundRepo) RenewResourceRentalClaim(ctx context.Context, claim outboundapp.ResourceRentalClaim) error {
	return mutateResourceRentalClaim(ctx, claim.RentalID, &claim, func(ctx context.Context) error {
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_resource_rentals SET lease_expires_at=clock_timestamp()+($2*interval '1 second'),updated_at=NOW() WHERE id=$1`, claim.RentalID, int64(outboundSettlementLease.Seconds()))
		return err
	})
}

func (r *OutboundRepo) MarkResourceRentalClaimReclaiming(ctx context.Context, claim outboundapp.ResourceRentalClaim) error {
	return r.markResourceRentalReclaiming(ctx, claim.RentalID, &claim)
}

func (r *OutboundRepo) MarkResourceRentalClaimHeld(ctx context.Context, claim outboundapp.ResourceRentalClaim, reason string, responseJSON []byte) error {
	return r.markResourceRentalHeld(ctx, claim.RentalID, &claim, reason, responseJSON)
}

func (r *OutboundRepo) CloseResourceRentalClaim(ctx context.Context, claim outboundapp.ResourceRentalClaim, refund types.RawAmount, providerBalance *types.RawAmount, responseJSON []byte, status string) error {
	return r.closeResourceRental(ctx, claim.RentalID, &claim, refund, providerBalance, responseJSON, status)
}

func (r *OutboundRepo) ReleaseResourceRentalClaim(ctx context.Context, claim outboundapp.ResourceRentalClaim) error {
	return mutateResourceRentalClaim(ctx, claim.RentalID, &claim, func(ctx context.Context) error { return clearResourceRentalClaim(ctx, claim.RentalID) })
}
