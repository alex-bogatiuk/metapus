package crypto_repo

import (
	"context"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

type ResourceRentalWorkflowRepo struct{}

func NewResourceRentalWorkflowRepo() *ResourceRentalWorkflowRepo {
	return &ResourceRentalWorkflowRepo{}
}

func resourceRentalQuerier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

func (r *ResourceRentalWorkflowRepo) GetResourceRentalMetrics(ctx context.Context) (cryptoapp.ResourceRentalMetrics, error) {
	var value cryptoapp.ResourceRentalMetrics
	q := resourceRentalQuerier(ctx)
	err := q.QueryRow(ctx, `SELECT
		COUNT(*) FILTER(WHERE status='active'),
		COUNT(*) FILTER(WHERE status='active' AND expires_at<=NOW()+interval '5 minutes'),
		COALESCE(SUM(reserved_amount) FILTER(WHERE status IN('active','reclaiming','held')),0),
		COALESCE(SUM(GREATEST(delegated_amount-consumed_amount,0)) FILTER(WHERE status IN('active','reclaiming','held')),0),
		COALESCE(SUM(allocated_cost_amount),0),COALESCE(SUM(unused_cost_amount),0),
		COUNT(*) FILTER(WHERE status='held' AND reclaim_requested_at IS NOT NULL)
		FROM doc_crypto_resource_rentals WHERE deletion_mark=FALSE`).Scan(
		&value.ActiveRentals, &value.ExpiringRentals, &value.ReservedEnergy, &value.UnusedEnergy,
		&value.AllocatedCost, &value.UnusedCost, &value.ReclaimFailures,
	)
	if err != nil {
		return value, err
	}
	if err := q.QueryRow(ctx, `SELECT COALESCE(SUM(delta_amount),0) FROM reg_crypto_provider_balance_incidents WHERE status IN('open','acknowledged')`).Scan(&value.ProviderBalanceDelta); err != nil {
		return value, err
	}
	err = q.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM doc_crypto_withdrawals WHERE settlement_status IN('unknown','repairing','held') AND tx_hash<>'')+
		(SELECT COUNT(*) FROM doc_treasury_withdrawals WHERE settlement_status IN('unknown','repairing','held') AND tx_hash<>'')+
		(SELECT COUNT(*) FROM doc_crypto_sweeps__lines WHERE settlement_status IN('unknown','repairing','held') AND tx_hash<>'')`).Scan(&value.SettlementRepairBacklog)
	return value, err
}

func (r *ResourceRentalWorkflowRepo) ListOpenProviderBalanceIncidents(ctx context.Context) ([]cryptoapp.ProviderBalanceIncident, error) {
	rows, err := resourceRentalQuerier(ctx).Query(ctx, `SELECT i.id,i.provider_account_id,i.token_id,i.status,i.ledger_amount,i.provider_amount,i.delta_amount,i.first_seen_at,i.last_seen_at,i.occurrence_count,i.resolution_reason,i.resolved_at,obs.id,obs.observed_at
 FROM reg_crypto_provider_balance_incidents i
 LEFT JOIN LATERAL (SELECT id,observed_at FROM reg_crypto_provider_balance_observations
     WHERE provider_account_id=i.provider_account_id AND token_id=i.token_id ORDER BY observed_at DESC,id DESC LIMIT 1) obs ON TRUE
 WHERE i.status IN('open','acknowledged') ORDER BY i.last_seen_at DESC,i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]cryptoapp.ProviderBalanceIncident, 0)
	for rows.Next() {
		var item cryptoapp.ProviderBalanceIncident
		if err := rows.Scan(&item.ID, &item.ProviderAccountID, &item.TokenID, &item.Status, &item.LedgerAmount, &item.ProviderAmount, &item.DeltaAmount, &item.FirstSeenAt, &item.LastSeenAt, &item.OccurrenceCount, &item.ResolutionReason, &item.ResolvedAt, &item.ObservationID, &item.ObservedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ResourceRentalWorkflowRepo) AcknowledgeProviderBalanceIncident(ctx context.Context, incidentID id.ID) (bool, error) {
	tag, err := resourceRentalQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_provider_balance_incidents SET status='acknowledged',updated_at=NOW() WHERE id=$1 AND status='open'`, incidentID)
	return tag.RowsAffected() == 1, err
}

func (r *ResourceRentalWorkflowRepo) ListResourceAllocations(ctx context.Context, rentalID id.ID) ([]cryptoapp.ResourceAllocation, error) {
	rows, err := resourceRentalQuerier(ctx).Query(ctx, `SELECT id,basis_type,basis_id,purpose,operation_token_id,source_wallet_id,status,reserved_amount,actual_amount,allocated_cost_amount,tx_hash,chain_result,reserved_at FROM reg_crypto_resource_allocations WHERE rental_id=$1 ORDER BY reserved_at,id`, rentalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]cryptoapp.ResourceAllocation, 0)
	for rows.Next() {
		var item cryptoapp.ResourceAllocation
		if err := rows.Scan(&item.ID, &item.BasisType, &item.BasisID, &item.Purpose, &item.OperationTokenID, &item.SourceWalletID, &item.Status, &item.ReservedAmount, &item.ActualAmount, &item.AllocatedCostAmount, &item.TxHash, &item.ChainResult, &item.ReservedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ResourceRentalWorkflowRepo) QueueResourceRentalReclaim(ctx context.Context, rentalID, actorID id.ID) (bool, error) {
	tag, err := resourceRentalQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_resource_rentals SET expires_at=NOW(),claimed_by='',claimed_at=NULL,lease_expires_at=NULL,updated_by=$2,updated_at=NOW(),version=version+1 WHERE id=$1 AND status='active' AND deletion_mark=FALSE`, rentalID, actorID)
	return tag.RowsAffected() == 1, err
}

func (r *ResourceRentalWorkflowRepo) RetryHeldResourceRental(ctx context.Context, rentalID, actorID id.ID) (bool, error) {
	tag, err := resourceRentalQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_resource_rentals SET status=CASE WHEN external_serial='' THEN 'ordering' WHEN reclaim_requested_at IS NOT NULL THEN 'reclaiming' ELSE 'active' END,error_message='',claimed_by='',claimed_at=NULL,lease_expires_at=NULL,updated_by=$2,updated_at=NOW(),version=version+1 WHERE id=$1 AND status='held' AND deletion_mark=FALSE`, rentalID, actorID)
	return tag.RowsAffected() == 1, err
}
