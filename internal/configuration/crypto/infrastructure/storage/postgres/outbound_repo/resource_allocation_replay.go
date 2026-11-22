package outbound_repo

import (
	"context"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

func validateResourceReservationReplay(ctx context.Context, existing *ResourceAllocationRecord, req ResourceRequest, required int64) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `SELECT id FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, existing.RentalID); err != nil {
		return err
	}
	var matches bool
	err := q.QueryRow(ctx, `SELECT a.status,a.rental_id=$2 AND a.generation=$3 AND a.basis_type=$4 AND a.basis_id=$5 AND a.purpose=$6
 AND a.operation_token_id=$7 AND a.source_wallet_id=$8 AND a.resource_type='energy' AND a.reserved_amount=$9
 AND a.status IN ('reserved','broadcast','confirmed') AND r.network_id=$10 AND r.receive_wallet_id=$8 AND w.network_id=$10 AND t.network_id=$10
 FROM reg_crypto_resource_allocations a JOIN doc_crypto_resource_rentals r ON r.id=a.rental_id JOIN cat_wallets w ON w.id=a.source_wallet_id JOIN cat_tokens t ON t.id=a.operation_token_id
	 WHERE a.id=$1 FOR UPDATE OF a`, existing.ID, existing.RentalID, existing.Generation, req.BasisType, req.BasisID, req.Purpose, req.OperationTokenID, req.SourceWalletID, required, req.NetworkID).Scan(&existing.Status, &matches)
	if err != nil {
		return err
	}
	if !matches {
		return apperror.NewConflict("resource reservation replay differs from its original owner, asset or quantity")
	}
	return nil
}

func validateResourceBroadcastEvent(ctx context.Context, allocation id.ID, hash string) error {
	var matches bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT count(*) FILTER(WHERE old_status<>'broadcast')=1
 AND COALESCE(bool_and(COALESCE(((event_type='broadcast' AND old_status IN ('reserved','broadcast')) OR (event_type='broadcast_recovered' AND old_status='released'))
 AND new_status='broadcast' AND details->>'txHash'=$2 AND details->>'exactAuthorization'='true',FALSE)),FALSE)
 FROM reg_crypto_resource_allocation_events WHERE allocation_id=$1 AND event_type IN ('broadcast','broadcast_recovered')`, allocation, hash).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return apperror.NewConflict("resource broadcast event differs from its original signed transaction; reconciliation required")
	}
	return nil
}

func validateResourceReleaseReplay(ctx context.Context, key, rental id.ID, reason string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var exists, matches bool
	if err := q.QueryRow(ctx, `SELECT count(*)>0,count(*)=1 AND COALESCE(bool_and(COALESCE(old_status='reserved' AND new_status='released' AND details->>'reason'=$2,FALSE)),FALSE)
 FROM reg_crypto_resource_allocation_events WHERE allocation_id=$1 AND event_type='released'`, key, reason).Scan(&exists, &matches); err != nil {
		return err
	}
	if exists {
		if !matches {
			return apperror.NewConflict("resource release replay differs from its original reason")
		}
		return nil
	}
	// Close owns bulk release via its complete terminal rental fact. A cleanup
	// call cannot invent an allocation release reason for that different owner.
	var terminal bool
	var f rentalFinancialIdentity
	f.rental = rental
	if err := q.QueryRow(ctx, `SELECT status IN ('closed','failed') AND closed_at IS NOT NULL AND posted AND posted_version=1,
 provider_account_id,network_id,cost_token_id,provider,external_serial,gross_cost_amount,refund_amount FROM doc_crypto_resource_rentals WHERE id=$1`, rental).Scan(&terminal, &f.account, &f.network, &f.token, &f.provider, &f.serial, &f.gross, &f.refund); err != nil {
		return err
	}
	if !terminal {
		return apperror.NewConflict("resource release has no original decision; reconciliation required")
	}
	return validateRentalFinancials(ctx, q, f)
}
