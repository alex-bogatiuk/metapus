package outbound_repo

import (
	"context"
	"errors"
	"strings"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// The shared workflow/operation -> policy -> scopes/wallets -> task prefix
// must be held before this check. Read wall time after any lock wait.
func validateActivationCostClaim(ctx context.Context, task outboundapp.ActivationCostRepairTask) error {
	if id.IsNil(task.CostClaimToken) || strings.TrimSpace(task.TxHash) == "" {
		return apperror.NewConflict("activation cost repair claim is missing or stale")
	}
	var valid bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM reg_crypto_account_activations a JOIN doc_crypto_network_operations o ON o.id=a.network_operation_id
 WHERE a.id=$1 AND a.network_id=$2 AND a.cost_claim_token=$3 AND a.cost_status='repairing'
 AND a.cost_lease_expires_at>clock_timestamp() AND o.tx_hash=$4 AND o.network_id=a.network_id
 AND o.target_wallet_id=a.wallet_id AND o.basis_type='CryptoAccountActivationTask' AND o.basis_id=a.id)`, task.ActivationID, task.NetworkID, task.CostClaimToken, task.TxHash).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("activation cost repair claim is missing or stale")
	}
	return nil
}

func (r *OutboundRepo) PrepareActivationCostRepair(ctx context.Context, task outboundapp.ActivationCostRepairTask) error {
	return mutateActivationState(ctx, task.ActivationID, func(ctx context.Context) error {
		if err := validateActivationCostClaim(ctx, task); err != nil {
			return err
		}
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_account_activations SET cost_lease_expires_at=clock_timestamp()+($2*interval '1 second'),updated_at=NOW() WHERE id=$1`, task.ActivationID, int64(_activationCostRepairLease.Seconds()))
		return err
	})
}

func (r *OutboundRepo) MarkActivationCostRepairHeld(ctx context.Context, task outboundapp.ActivationCostRepairTask, reason string) error {
	return mutateActivationState(ctx, task.ActivationID, func(ctx context.Context) error {
		if err := validateActivationCostClaim(ctx, task); err != nil {
			return err
		}
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_account_activations SET cost_status='held',cost_checked_at=clock_timestamp(),cost_error=$2,
 cost_claim_token=NULL,cost_claimed_by='',cost_claimed_at=NULL,cost_lease_expires_at=NULL,updated_at=NOW() WHERE id=$1`, task.ActivationID, statusFailedReason(errors.New(reason)))
		return err
	})
}
