package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *AMLRepository) ConfigureAMLBatchPolicy(ctx context.Context, request aml.BatchPolicyRequest) (int64, error) {
	if err := request.Validate(); err != nil {
		return 0, err
	}
	var version int64
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		if _, err := q.Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 FOR UPDATE`, request.MerchantID, request.NetworkID, request.TokenID); err != nil {
			return err
		}
		var current int64
		if err := q.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM sys_crypto_aml_batch_policies WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 AND risk_class=$4`, request.MerchantID, request.NetworkID, request.TokenID, request.Class).Scan(&current); err != nil {
			return err
		}
		if current != request.ExpectedVersion {
			return apperror.NewConflict("AML batch policy version changed")
		}
		return q.QueryRow(ctx, `INSERT INTO sys_crypto_aml_batch_policies(merchant_id,network_id,token_id,risk_class,version,max_gross,max_age_seconds,configured_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING version`, request.MerchantID, request.NetworkID, request.TokenID, request.Class, current+1, request.MaxGross, request.MaxAgeSeconds, request.Actor).Scan(&version)
	})
	return version, err
}

func (r *AMLRepository) OpenAMLBatch(ctx context.Context, request aml.OpenBatchRequest) (aml.Batch, error) {
	var result aml.Batch
	if err := request.Validate(); err != nil {
		return result, err
	}
	raw, err := json.MarshalDeterministic(request)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		if _, err := q.Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 FOR UPDATE`, request.MerchantID, request.NetworkID, request.TokenID); err != nil {
			return err
		}
		var stored string
		err := q.QueryRow(ctx, `SELECT request_digest FROM reg_crypto_aml_batches WHERE id=$1`, request.ID).Scan(&stored)
		if err == nil {
			if stored != digest {
				return apperror.NewConflict("AML batch identity changed")
			}
			result, err = r.ReadAMLBatch(ctx, request.MerchantID, request.ID)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_batches(id,merchant_id,network_id,token_id,risk_class,policy_version,scope_version,collector_wallet_id,max_gross,intake_closes_at,request_digest,created_by,last_actor)
 SELECT $1,$2,$3,$4,$5,version,$6,$7,max_gross,NOW()+make_interval(secs=>max_age_seconds),$8,$9,$9 FROM sys_crypto_aml_batch_policies WHERE merchant_id=$2 AND network_id=$3 AND token_id=$4 AND risk_class=$5 AND version=$10`, request.ID, request.MerchantID, request.NetworkID, request.TokenID, request.Class, request.ScopeVersion, request.CollectorID, digest, request.Actor, request.PolicyVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("AML batch policy unavailable")
		}
		result, err = r.ReadAMLBatch(ctx, request.MerchantID, request.ID)
		return err
	})
	return result, err
}

func (r *AMLRepository) ReadAMLBatch(ctx context.Context, merchantID, batchID id.ID) (aml.Batch, error) {
	var b aml.Batch
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT b.id,b.collector_wallet_id,b.revision,b.execution_revision,b.max_gross,usage.committed,usage.reserved,usage.unplanned,b.intake_closes_at,b.intake_closed_at,b.execution_held
 FROM reg_crypto_aml_batches b JOIN crypto_aml_batch_usage usage ON usage.id=b.id WHERE b.id=$1 AND b.merchant_id=$2`, batchID, merchantID).Scan(&b.ID, &b.CollectorID, &b.Revision, &b.ExecutionRevision, &b.MaxGross, &b.Committed, &b.Reserved, &b.Unplanned, &b.IntakeClosesAt, &b.IntakeClosedAt, &b.ExecutionHeld)
	return b, err
}

func (r *AMLRepository) CloseAMLBatch(ctx context.Context, merchantID, batchID id.ID, expected int64, reason, actor string) error {
	if expected <= 0 || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 || strings.TrimSpace(actor) == "" {
		return apperror.NewValidation("batch close requires revision, reason and actor")
	}
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		if _, err := q.Exec(ctx, `SELECT s.version FROM reg_crypto_aml_batches b JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=b.merchant_id AND s.network_id=b.network_id AND s.token_id=b.token_id WHERE b.id=$1 AND b.merchant_id=$2 FOR UPDATE OF s`, batchID, merchantID); err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `UPDATE reg_crypto_aml_batches SET intake_closed_at=NOW(),close_reason=$4,last_actor=$5 WHERE id=$1 AND merchant_id=$2 AND revision=$3 AND intake_closed_at IS NULL`, batchID, merchantID, expected, reason, actor)
		if err == nil && tag.RowsAffected() != 1 {
			return apperror.NewConflict("AML batch already closed or revision changed")
		}
		return err
	})
}

// ReserveAMLBatchSweep chooses an owned collector and reserves lifetime intake
// and the exact principal/fee split in one transaction. It grants no dispatch.
func (r *AMLRepository) ReserveAMLBatchSweep(ctx context.Context, request aml.BatchSweepRequest) (aml.InternalAllocation, error) {
	var result aml.InternalAllocation
	if err := request.Validate(); err != nil {
		return result, err
	}
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var version int64
		if err := q.QueryRow(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 AND enabled FOR UPDATE`, request.MerchantID, request.NetworkID, request.TokenID).Scan(&version); err != nil {
			return err
		}
		var batchID, destination id.ID
		var revision, executionRevision int64
		err := q.QueryRow(ctx, `SELECT batch_id,destination_wallet_id,batch_revision,batch_execution_revision FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=$1 AND batch_id IS NOT NULL`, request.SweepAttemptID).Scan(&batchID, &destination, &revision, &executionRevision)
		if errors.Is(err, pgx.ErrNoRows) {
			if version != request.ScopeVersion {
				return apperror.NewConflict("AML batch scope revision changed")
			}
			err = q.QueryRow(ctx, `SELECT b.id,b.collector_wallet_id,b.revision,b.execution_revision FROM reg_crypto_aml_batches b JOIN crypto_aml_batch_usage usage ON usage.id=b.id
 WHERE b.merchant_id=$1 AND b.network_id=$2 AND b.token_id=$3 AND b.risk_class=$4 AND b.scope_version=$5 AND NOT b.execution_held AND b.intake_closed_at IS NULL AND b.intake_closes_at>clock_timestamp()
 AND usage.used+$6<=b.max_gross AND fn_crypto_aml_total_inventory_covered(b.collector_wallet_id,b.token_id)
 ORDER BY b.opened_at,b.id FOR UPDATE OF b LIMIT 1`, request.MerchantID, request.NetworkID, request.TokenID, request.Class, request.ScopeVersion, request.Amount).Scan(&batchID, &destination, &revision, &executionRevision)
			if errors.Is(err, pgx.ErrNoRows) {
				return apperror.NewConflict("no ready AML batch capacity; provision an owned collector")
			}
			if err != nil {
				return err
			}
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
 SELECT $1,sw.id,$2,sw.network_id,dw.id,sw.address,dw.address,$3,'claimed',NOW()+interval '15 minutes' FROM cat_wallets sw JOIN cat_wallets dw ON dw.id=$4 WHERE sw.id=$5 AND sw.network_id=$6 AND sw.custody_merchant_id=$7`, request.SweepAttemptID, request.TokenID, request.Amount, destination, request.SourceWalletID, request.NetworkID, request.MerchantID)
		}
		if err != nil {
			return err
		}
		result, err = r.ReserveAMLInternalAllocation(ctx, aml.InternalAllocationRequest{SweepAttemptID: request.SweepAttemptID, MerchantID: request.MerchantID, NetworkID: request.NetworkID, TokenID: request.TokenID,
			SourceWalletID: request.SourceWalletID, DestinationID: destination, Amount: request.Amount, SourceRevision: request.SourceRevision, RiskRevision: request.RiskRevision, ScopeVersion: request.ScopeVersion, Class: request.Class,
			BatchID: &batchID, BatchRevision: revision, BatchExecutionRevision: executionRevision})
		return err
	})
	return result, err
}

func (r *AMLRepository) CloseNextExpiredAMLBatch(ctx context.Context) (bool, error) {
	changed := false
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var batchID id.ID
		err := q.QueryRow(ctx, `SELECT b.id FROM reg_crypto_aml_batches b JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=b.merchant_id AND s.network_id=b.network_id AND s.token_id=b.token_id
 WHERE b.intake_closed_at IS NULL AND b.intake_closes_at<=clock_timestamp() ORDER BY b.intake_closes_at,b.id FOR UPDATE OF s SKIP LOCKED LIMIT 1`).Scan(&batchID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `UPDATE reg_crypto_aml_batches SET intake_closed_at=NOW(),close_reason='age',last_actor='aml-maintenance' WHERE id=$1 AND intake_closed_at IS NULL`, batchID)
		changed = tag.RowsAffected() == 1
		return err
	})
	return changed, err
}
