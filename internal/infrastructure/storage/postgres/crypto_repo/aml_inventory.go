package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

// CaptureNextAMLInventoryAllocation consumes a durable local queue. Missing
// evidence is not guessed; a later financial/proof update enqueues it again.
// Neither a payment allocation nor a transfer creates a financial movement.
func (r *AMLRepository) CaptureNextAMLInventoryAllocation(ctx context.Context) (bool, error) {
	progressed := false
	var captureErr error
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) != nil {
		// The wakeup barrier relies on a fresh statement snapshot after the
		// scope lock. A repeatable-read/serializable caller could acknowledge
		// an old incomplete snapshot after the last producer already committed.
		var isolation string
		if err := tm.GetQuerier(ctx).QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return false, err
		}
		if isolation != string(pgx.ReadCommitted) {
			return false, apperror.NewValidation("inventory capture requires read committed isolation")
		}
	}
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var kind string
		var basisID id.ID
		// Producers use this same scope as a wakeup barrier. Keep it through
		// capture+ack; do not lock the queue row before acquiring the scope.
		// The next statement sees evidence committed before this scope lock.
		err := q.QueryRow(ctx, `SELECT pending.kind,pending.basis_id FROM reg_crypto_aml_inventory_queue pending
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=pending.merchant_id AND scope.network_id=pending.network_id AND scope.token_id=pending.token_id AND scope.enabled
 WHERE pending.next_attempt_at<=NOW() ORDER BY pending.next_attempt_at,pending.enqueued_at,pending.kind,pending.basis_id FOR UPDATE OF scope SKIP LOCKED LIMIT 1`).Scan(&kind, &basisID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		captureErr = tm.RunInSavepoint(ctx, func(ctx context.Context) error {
			switch kind {
			case "payment":
				_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_inventory_facts(kind,merchant_id,network_id,token_id,destination_wallet_id,principal,service_fee,payment_id,payment_version,effect_id,destination_movement_id)
 SELECT 'payment',merchant_id,network_id,token_id,wallet_id,principal,service_fee,payment_id,payment_version,effect_id,movement_id
 FROM crypto_aml_allocatable_payments WHERE payment_id=$1 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE payment_id=$1)`, basisID)
			case "transfer":
				_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_inventory_facts(kind,merchant_id,network_id,token_id,source_wallet_id,destination_wallet_id,principal,service_fee,allocation_id,effect_id,source_movement_id,destination_movement_id)
 SELECT 'transfer',merchant_id,network_id,token_id,source_wallet_id,destination_wallet_id,principal,service_fee,allocation_id,effect_id,source_movement_id,destination_movement_id
 FROM crypto_aml_allocatable_transfers WHERE allocation_id=$1 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE allocation_id=$1)`, basisID)
			case "resource":
				err = r.captureAMLResourceBudget(ctx, basisID)
			default:
				err = fmt.Errorf("unknown AML inventory queue kind %q", kind)
			}
			return err
		})
		if captureErr != nil {
			// A damaged projection cannot poison all merchants' allocation work.
			// The savepoint rolls back every component write; the durable queue
			// records a bounded retry without changing any financial movement.
			_, err = q.Exec(ctx, `UPDATE reg_crypto_aml_inventory_queue SET attempts=attempts+1,last_error='allocation_evidence_conflict',next_attempt_at=NOW()+interval '30 seconds' WHERE kind=$1 AND basis_id=$2`, kind, basisID)
			if err != nil {
				return err
			}
			captureErr = fmt.Errorf("capture AML inventory %s: %w", kind, captureErr)
			progressed = true
			return nil
		}
		if _, err := q.Exec(ctx, `DELETE FROM reg_crypto_aml_inventory_queue WHERE kind=$1 AND basis_id=$2`, kind, basisID); err != nil {
			return err
		}
		progressed = true
		return nil
	})
	return progressed, errors.Join(err, captureErr)
}

// ReserveAMLInternalAllocation freezes the ownership split before signing.
func (r *AMLRepository) ReserveAMLInternalAllocation(ctx context.Context, request aml.InternalAllocationRequest) (aml.InternalAllocation, error) {
	var result aml.InternalAllocation
	if err := request.Validate(); err != nil {
		return result, err
	}
	raw, err := json.MarshalDeterministic(request)
	if err != nil {
		return result, apperror.NewInternal(err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var version int64
		if err := q.QueryRow(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 AND enabled FOR UPDATE`, request.MerchantID, request.NetworkID, request.TokenID).Scan(&version); err != nil {
			return err
		}
		var storedDigest string
		err := q.QueryRow(ctx, `SELECT id,status,principal,service_fee,request_digest FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=$1`, request.SweepAttemptID).Scan(&result.ID, &result.Status, &result.Partition.Principal, &result.Partition.ServiceFee, &storedDigest)
		if err == nil {
			if storedDigest != digest {
				return apperror.NewConflict("sweep allocation request changed; create a new attempt")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if version != request.ScopeVersion {
			return apperror.NewConflict("AML allocation scope revision changed")
		}
		// SQL guard takes the same scope then ordered inventory locks and repeats
		// financial, proof, risk, identity and exact integer split checks.
		var available aml.InventoryPartition
		var revision int64
		if _, err := q.Exec(ctx, `SELECT wallet_id FROM reg_crypto_aml_inventory_positions WHERE token_id=$1 AND wallet_id=ANY($2) ORDER BY wallet_id FOR UPDATE`, request.TokenID, []id.ID{request.SourceWalletID, request.DestinationID}); err != nil {
			return err
		}
		err = q.QueryRow(ctx, `SELECT p.principal-COALESCE(reserved.principal,0),p.service_fee-COALESCE(reserved.service_fee,0),p.revision
 FROM reg_crypto_aml_inventory_positions p LEFT JOIN LATERAL(SELECT SUM(principal) principal,SUM(service_fee) service_fee FROM reg_crypto_aml_internal_allocations
 WHERE source_wallet_id=p.wallet_id AND token_id=p.token_id AND status='reserved') reserved ON TRUE WHERE p.wallet_id=$1 AND p.token_id=$2`, request.SourceWalletID, request.TokenID).Scan(&available.Principal, &available.ServiceFee, &revision)
		if err != nil {
			return err
		}
		if revision != request.SourceRevision {
			return apperror.NewConflict("AML inventory source revision changed")
		}
		result.Partition, err = aml.SplitInventory(available, request.Amount)
		if err != nil {
			return err
		}
		err = q.QueryRow(ctx, `INSERT INTO reg_crypto_aml_internal_allocations(sweep_attempt_id,merchant_id,network_id,token_id,source_wallet_id,destination_wallet_id,principal,service_fee,source_revision,risk_revision,scope_version,risk_class,request_digest,batch_id,batch_revision,batch_execution_revision)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,0),NULLIF($16,0)) RETURNING id,status`, request.SweepAttemptID, request.MerchantID, request.NetworkID, request.TokenID, request.SourceWalletID, request.DestinationID,
			result.Partition.Principal, result.Partition.ServiceFee, request.SourceRevision, request.RiskRevision, request.ScopeVersion, request.Class, digest, request.BatchID, request.BatchRevision, request.BatchExecutionRevision).Scan(&result.ID, &result.Status)
		return err
	})
	return result, err
}

// CancelAMLInternalAllocation only releases an unsigned, never-authorized plan.
// Unknown signature/broadcast outcomes retain the original reservation.
func (r *AMLRepository) CancelAMLInternalAllocation(ctx context.Context, allocationID id.ID, reason, actor string) error {
	if id.IsNil(allocationID) || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 || strings.TrimSpace(actor) == "" {
		return apperror.NewValidation("allocation cancellation requires a reason and actor")
	}
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var merchantID, networkID, tokenID id.ID
		if err := q.QueryRow(ctx, `SELECT merchant_id,network_id,token_id FROM reg_crypto_aml_internal_allocations WHERE id=$1`, allocationID).Scan(&merchantID, &networkID, &tokenID); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 FOR UPDATE`, merchantID, networkID, tokenID); err != nil {
			return err
		}
		var status string
		err := q.QueryRow(ctx, `SELECT status FROM reg_crypto_aml_internal_allocations WHERE id=$1 FOR UPDATE`, allocationID).Scan(&status)
		if err != nil {
			return err
		}
		if status == "cancelled" {
			return recordAMLCancellation(ctx, false, allocationID, reason, actor, false)
		}
		if status != "reserved" {
			return apperror.NewConflict("applied AML allocation cannot be cancelled")
		}
		if err := recordAMLCancellation(ctx, false, allocationID, reason, actor, true); err != nil {
			return err
		}

		if _, err = q.Exec(ctx, `UPDATE reg_crypto_aml_internal_allocations SET status='cancelled' WHERE id=$1`, allocationID); err != nil {
			return err
		}
		_, err = q.Exec(ctx, `UPDATE reg_crypto_sweep_attempts SET status='expired' WHERE id=(SELECT sweep_attempt_id FROM reg_crypto_aml_internal_allocations WHERE id=$1) AND status IN ('claimed','resource_ready')`, allocationID)
		return err
	})
}

// CancelSweepAttemptAllocation cancels the internal AML allocation associated with a sweep attempt.
func (r *AMLRepository) CancelSweepAttemptAllocation(ctx context.Context, sweepAttemptID id.ID, reason, actor string) error {
	if id.IsNil(sweepAttemptID) {
		return nil
	}
	var allocationID id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT id FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id = $1
	`, sweepAttemptID).Scan(&allocationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lookup sweep attempt allocation: %w", err)
	}
	return r.CancelAMLInternalAllocation(ctx, allocationID, reason, actor)
}
