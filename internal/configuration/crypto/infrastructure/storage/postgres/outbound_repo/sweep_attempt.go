package outbound_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	_sweepAttemptBasisType = "CryptoSweepAttempt"
	_sweepLineBasisType    = "CryptoSweepLine"
	_sweepAttemptRetryAge  = 30 * time.Second

	sweepAttemptStatusClaimed       = "claimed"
	sweepAttemptStatusResourceReady = "resource_ready"
	sweepAttemptStatusSigning       = "signing"
	sweepAttemptStatusBroadcast     = "broadcast"
	sweepAttemptStatusPosted        = "posted"
	sweepAttemptStatusHeld          = "held"
	sweepAttemptStatusFailed        = "failed"
	sweepAttemptStatusExpired       = "expired"
)

func (r *OutboundRepo) ClaimRunnableSweepAttempts(ctx context.Context, limit int) ([]SweepTask, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}

	cutoff := time.Now().UTC().Add(-_sweepAttemptRetryAge)
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		WITH picked AS (
			SELECT a.id, a.status=$9 AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations sa
                    WHERE sa.basis_type=$10 AND sa.basis_id=a.id AND sa.signed_envelope IS NOT NULL) AS recovery_only
            FROM reg_crypto_sweep_attempts a
			JOIN cat_wallets w ON w.id = a.wallet_id
			WHERE (
			      a.status IN ($1, $2, $3, $4)
                  OR (a.status=$9 AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations sa
                      WHERE sa.basis_type=$10 AND sa.basis_id=a.id AND sa.signed_envelope IS NOT NULL))
                  OR (a.status=$9 AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations sa
                    JOIN reg_crypto_fee_reservations fee ON fee.basis_type=sa.basis_type AND fee.basis_id=sa.basis_id AND fee.purpose=sa.purpose AND fee.attempt=sa.attempt
                    WHERE sa.basis_type=$10 AND sa.basis_id=a.id AND sa.status='created' AND sa.signed_envelope IS NULL
                    AND (sa.expires_at>NOW() OR EXISTS(SELECT 1 FROM reg_crypto_aml_authorization_renewals renewal WHERE renewal.authorization_id=sa.id AND renewal.expires_at>NOW())) AND sa.authorization_jws<>'' AND sa.unsigned_envelope IS NOT NULL
                    AND fee.status='reserved' AND fee.tx_hash=''))
			      OR (
			          a.status = $9
			          AND NOT EXISTS (
			              SELECT 1
			              FROM reg_crypto_signing_authorizations s
			              WHERE s.basis_type = $10
			                AND s.basis_id = a.id
			          )
			          AND (
			              EXISTS (
			                  SELECT 1
			              FROM doc_crypto_resource_rentals r
			              WHERE r.basis_type = $11
			                AND r.basis_id = a.sweep_id
			                AND r.receive_wallet_id = a.wallet_id
			                AND r.id = (
			                    SELECT latest.id
			                    FROM doc_crypto_resource_rentals latest
			                    WHERE latest.basis_type = $11
			                      AND latest.basis_id = a.sweep_id
			                      AND latest.receive_wallet_id = a.wallet_id
			                      AND latest.deletion_mark = FALSE
			                    ORDER BY latest.created_at DESC, latest.id DESC
			                    LIMIT 1
			                )
			                AND (
			                    (r.status IN ('created', 'ordering', 'held') AND r.external_serial = '')
			                    OR r.status IN ('active', 'closed')
			                )
			          )
			              OR (
			                  NOT EXISTS (
			                      SELECT 1
			                      FROM reg_crypto_fee_reservations f
			                      WHERE f.basis_type = $10
			                        AND f.basis_id = a.id
			                        AND f.purpose = $13
			                        AND f.attempt = 1
			                  )
			                  AND NOT EXISTS (
			                      SELECT 1
			                      FROM doc_crypto_resource_rentals r
			                      WHERE r.basis_type = $11
			                        AND r.basis_id = a.sweep_id
			                        AND r.receive_wallet_id = a.wallet_id
			                        AND r.deletion_mark = FALSE
			                  )
				              AND EXISTS (
				                  SELECT 1
				                  FROM sys_crypto_network_policies p
				                  LEFT JOIN sys_crypto_external_resource_policies tr
				                    ON tr.network_id = p.network_id
				                  WHERE p.network_id = a.network_id
				                    AND p.enabled = TRUE
				                    AND (p.updated_at > a.updated_at OR tr.updated_at > a.updated_at)
			                  )
			              )
			          )
			      )
			  )
			  AND (
			      a.status IN ($1, $2)
			      OR a.updated_at < $5
			  )
			  AND (a.expires_at > NOW() OR a.status IN ($9, $4)
			       OR EXISTS (SELECT 1 FROM reg_crypto_signing_authorizations sa
			                  WHERE sa.basis_type=$10 AND sa.basis_id=a.id
			                    AND sa.status <> 'failed')
			       OR EXISTS (SELECT 1 FROM doc_crypto_native_fundings funding
			                  WHERE funding.basis_type=$10 AND funding.basis_id=a.id
			                    AND funding.status <> 'failed'))
			  AND ((w.status=$6 AND w.chain_activation_status=$8 AND w.is_active AND NOT w.deletion_mark)
                   OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations sa WHERE sa.basis_type=$10 AND sa.basis_id=a.id AND sa.signed_envelope IS NOT NULL))
			ORDER BY a.updated_at, a.id
			FOR UPDATE OF a SKIP LOCKED
			LIMIT $7
		),
		updated AS (
			UPDATE reg_crypto_sweep_attempts a
			SET status = CASE
                    WHEN picked.recovery_only THEN $9
			        WHEN a.status = $4 THEN $4
			        WHEN a.status = $9 THEN $1
			        ELSE $3
			    END,
			    expires_at = CASE
			        WHEN a.status = $9 THEN NOW() + ($12 * interval '1 second')
			        ELSE a.expires_at
			    END,
			    updated_at = NOW()
			FROM picked
			WHERE a.id = picked.id
			RETURNING a.id, a.wallet_id, a.token_id, a.network_id, a.hot_wallet_id,
			          a.source_address, a.destination_address, a.amount, a.status, a.sweep_id,picked.recovery_only
		)
		SELECT u.id,
		       u.sweep_id,
		       u.wallet_id,
		       u.token_id,
		       u.network_id,
		       COALESCE(n.code, '') AS network_code,
		       u.source_address,
		       w.vault_key_id,
		       COALESCE(t.contract_address, '') AS contract_address,
		       t.token_standard,
		       t.decimal_places,
		       w.chain_activation_status,
		       u.amount,
		       COALESCE(n.confirmations_needed, 1) AS required_confs,
		       u.hot_wallet_id,
		       u.destination_address,
		       hw.vault_key_id,
		       COALESCE(hb.amount, 0) AS hot_wallet_balance,u.recovery_only
		FROM updated u
		JOIN cat_wallets w ON w.id = u.wallet_id
		JOIN cat_wallets hw ON hw.id = u.hot_wallet_id
		JOIN cat_tokens t ON t.id = u.token_id
		JOIN cat_blockchain_networks n ON n.id = u.network_id
		LEFT JOIN reg_crypto_balance_balances hb
		  ON hb.wallet_id = hw.id AND hb.token_id = u.token_id
		ORDER BY u.network_id, u.token_id, u.amount DESC
		`, sweepAttemptStatusClaimed, sweepAttemptStatusResourceReady, sweepAttemptStatusSigning,
		sweepAttemptStatusBroadcast, cutoff, wallet.WalletStatusSweeping, limit, wallet.ChainActivationActive,
		sweepAttemptStatusHeld, _sweepAttemptBasisType, crypto_sweep.DocumentType,
		int64(_signingAuthorizationTTL.Seconds()), crypto.SigningPurposeSweep)
	if err != nil {
		return nil, fmt.Errorf("claim runnable sweep attempts: %w", err)
	}
	defer rows.Close()

	return scanSweepTaskRows(rows, limit)
}

func (r *OutboundRepo) ExpireStaleSweepAttempts(ctx context.Context) error {
	return cryptoapp.NewSweepLifecycleService(contextTxManager{}, r).Maintain(ctx)
}

func (r *OutboundRepo) ExpireUnsignedSweepAttempts(ctx context.Context, batch cryptoapp.SweepMaintenanceBatch) error {
	txm := postgres.MustGetTxManager(ctx)
	if txm.GetTx(ctx) == nil {
		return apperror.NewValidation("sweep expiry requires the maintenance transaction")
	}
	querier := txm.GetQuerier(ctx)

	// 1. Mark expired unsigned authorizations as failed.
	_, err := querier.Exec(ctx, `
		UPDATE reg_crypto_signing_authorizations s
		SET status = 'failed',
		    updated_at = NOW()
		WHERE id=ANY($1::uuid[]) AND status IN ('created','held')
		  AND signed_envelope IS NULL
		  AND expires_at <= NOW()
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_aml_authorization_renewals r
		      WHERE r.authorization_id = s.id AND r.expires_at > NOW()
		  )
	`, batch.Authorizations)
	if err != nil {
		return fmt.Errorf("fail expired signing authorizations: %w", err)
	}

	// 2. Cancel operating resource budgets for failed unsigned authorizations.
	_, err = querier.Exec(ctx, `
		INSERT INTO reg_crypto_aml_resource_cancellations (budget_id, reason, cancelled_by)
		SELECT b.id, 'signing authorization expired before transaction signing', 'sweep_lifecycle'
		FROM reg_crypto_aml_resource_budgets b
		JOIN reg_crypto_fee_reservations f ON f.id = b.fee_reservation_id
		JOIN reg_crypto_signing_authorizations s ON s.basis_type = f.basis_type
		  AND s.basis_id = f.basis_id
		  AND s.purpose = f.purpose
		  AND s.attempt = f.attempt
		WHERE b.id=ANY($1::uuid[]) AND s.status = 'failed'
		  AND s.signed_envelope IS NULL
		  AND b.status = 'reserved'
		ON CONFLICT (budget_id) DO NOTHING
	`, batch.Budgets)
	if err != nil {
		return fmt.Errorf("record resource cancellations for expired authorizations: %w", err)
	}
	var conflict bool
	if err := querier.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM reg_crypto_aml_resource_budgets b
		JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id
		JOIN reg_crypto_signing_authorizations s ON s.basis_type=f.basis_type AND s.basis_id=f.basis_id AND s.purpose=f.purpose AND s.attempt=f.attempt
		LEFT JOIN reg_crypto_aml_resource_cancellations c ON c.budget_id=b.id
		WHERE b.id=ANY($1::uuid[]) AND b.status='reserved' AND s.status='failed' AND s.signed_envelope IS NULL
		AND (c.budget_id IS NULL OR c.reason<>'signing authorization expired before transaction signing' OR c.cancelled_by<>'sweep_lifecycle' OR c._deleted_at IS NOT NULL))`, batch.Budgets).Scan(&conflict); err != nil {
		return fmt.Errorf("check expired resource cancellation replay: %w", err)
	}
	if conflict {
		return apperror.NewConflict("expired resource cancellation differs from its original reason or actor")
	}

	_, err = querier.Exec(ctx, `
		UPDATE reg_crypto_aml_resource_budgets b
		SET status = 'cancelled',
		    updated_at = NOW()
		FROM reg_crypto_fee_reservations f
		JOIN reg_crypto_signing_authorizations s ON s.basis_type = f.basis_type
		  AND s.basis_id = f.basis_id
		  AND s.purpose = f.purpose
		  AND s.attempt = f.attempt
		WHERE b.id=ANY($1::uuid[]) AND b.fee_reservation_id = f.id
		  AND s.status = 'failed'
		  AND s.signed_envelope IS NULL
		  AND b.status = 'reserved'
	`, batch.Budgets)
	if err != nil {
		return fmt.Errorf("cancel resource budgets for expired authorizations: %w", err)
	}

	// 3. Release unbroadcast fee reservations for failed authorizations.
	_, err = querier.Exec(ctx, `
		UPDATE reg_crypto_fee_reservations f
		SET status = 'released',
		    hold_reason = 'signing authorization expired before transaction signing',
		    released_at = NOW(),
		    updated_at = NOW()
		WHERE f.id=ANY($1::uuid[]) AND f.status IN ('reserved','held')
		  AND f.tx_hash = ''
		  AND EXISTS (
		      SELECT 1 FROM reg_crypto_signing_authorizations s
		      WHERE s.basis_type = f.basis_type
		        AND s.basis_id = f.basis_id
		        AND s.purpose = f.purpose
		        AND s.attempt = f.attempt
		        AND s.status = 'failed'
		        AND s.signed_envelope IS NULL
		  )
	`, batch.Fees)
	if err != nil {
		return fmt.Errorf("release fee reservations for expired authorizations: %w", err)
	}

	// 4. Expire stale sweep attempts.
	_, err = querier.Exec(ctx, `
			UPDATE reg_crypto_sweep_attempts a
			SET status = $1,
			    error_message = 'sweep attempt expired before broadcast',
			    updated_at = NOW()
			WHERE a.id=ANY($7::uuid[]) AND a.status IN ($2, $3, $4, $5)
			  AND a.expires_at <= NOW()
			  AND NOT EXISTS (
			      SELECT 1
			      FROM reg_crypto_signing_authorizations s
			      WHERE s.basis_type = $6
			        AND s.basis_id = a.id
			        AND s.status <> 'failed'
			  )
			  AND NOT EXISTS (
			      SELECT 1 FROM doc_crypto_native_fundings funding
			      WHERE funding.basis_type=$6 AND funding.basis_id=a.id
			        AND funding.status <> 'failed'
			  )
	`, sweepAttemptStatusExpired, sweepAttemptStatusClaimed, sweepAttemptStatusResourceReady,
		sweepAttemptStatusSigning, sweepAttemptStatusHeld, _sweepAttemptBasisType, batch.Attempts)
	if err != nil {
		return fmt.Errorf("expire stale sweep attempts: %w", err)
	}
	_, err = querier.Exec(ctx, `
		INSERT INTO reg_crypto_aml_allocation_cancellations (allocation_id, reason, cancelled_by)
		SELECT p.id, 'sweep attempt expired before broadcast', 'sweep_lifecycle'
		FROM reg_crypto_aml_internal_allocations p
		JOIN reg_crypto_sweep_attempts a ON a.id = p.sweep_attempt_id
		WHERE p.id=ANY($2::uuid[]) AND a.status = $1 AND p.status = 'reserved'
		ON CONFLICT DO NOTHING
	`, sweepAttemptStatusExpired, batch.Allocations)
	if err != nil {
		return fmt.Errorf("record allocation cancellations for expired sweeps: %w", err)
	}
	if err := querier.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM reg_crypto_aml_internal_allocations p
		JOIN reg_crypto_sweep_attempts a ON a.id=p.sweep_attempt_id
		LEFT JOIN reg_crypto_aml_allocation_cancellations c ON c.allocation_id=p.id
		WHERE p.id=ANY($2::uuid[]) AND a.status=$1 AND p.status='reserved'
		AND (c.allocation_id IS NULL OR c.reason<>'sweep attempt expired before broadcast' OR c.cancelled_by<>'sweep_lifecycle' OR c._deleted_at IS NOT NULL))`, sweepAttemptStatusExpired, batch.Allocations).Scan(&conflict); err != nil {
		return fmt.Errorf("check expired allocation cancellation replay: %w", err)
	}
	if conflict {
		return apperror.NewConflict("expired allocation cancellation differs from its original reason or actor")
	}
	_, err = querier.Exec(ctx, `
		UPDATE reg_crypto_aml_internal_allocations p
		SET status = 'cancelled'
		FROM reg_crypto_sweep_attempts a
		WHERE p.id=ANY($2::uuid[]) AND a.id = p.sweep_attempt_id AND a.status = $1 AND p.status = 'reserved'
	`, sweepAttemptStatusExpired, batch.Allocations)
	if err != nil {
		return fmt.Errorf("cancel allocations for expired sweeps: %w", err)
	}
	return nil
}

func (r *OutboundRepo) LockSweepWallets(ctx context.Context, walletIDs []id.ID) error {
	return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, walletIDs...)
}

// Close only unsigned, unposted drafts whose attempts are ALL terminal. Never
// revive an expired execution identity or hide a possibly broadcast envelope.
func (r *OutboundRepo) CloseAbandonedSweepDrafts(ctx context.Context, batch cryptoapp.SweepMaintenanceBatch) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE doc_crypto_sweeps d SET status='partial_failed',updated_at=NOW(),version=version+1
		WHERE d.id=ANY($1::uuid[]) AND d.status IN ('created','held') AND d.posted=FALSE
		  AND EXISTS (SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.sweep_id=d.id)
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.sweep_id=d.id
		      AND (a.status NOT IN ('expired','failed') OR EXISTS (
		          SELECT 1 FROM reg_crypto_signing_authorizations s
		          WHERE s.basis_type='CryptoSweepAttempt' AND s.basis_id=a.id AND s.status <> 'failed'
		      ) OR EXISTS (
		          SELECT 1 FROM doc_crypto_native_fundings f
		          WHERE f.basis_type='CryptoSweepAttempt' AND f.basis_id=a.id AND f.status NOT IN ('settled','failed')
		      ))
		  )
	`, batch.Drafts)
	return err
}

// Rebuild the operational projection after terminal retries or a crash. Holds
// remain owned by reconciliation; maintenance never resolves them. A completed
// JIT can outlive its expired parent: retry uses a NEW sweep identity and the
// already-accounted native balance, not another funding transaction.
func (r *OutboundRepo) RecoverIdleSweepWallets(ctx context.Context, batch cryptoapp.SweepMaintenanceBatch) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE cat_wallets w
		SET status='sweep_pending',updated_at=NOW(),version=version+1
		WHERE w.id=ANY($1::uuid[]) AND w.status IN ('sweeping','free','assigned') AND w.is_active AND NOT w.deletion_mark
		  AND w.tier='pool' AND w.wallet_role='merchant_pool'
		  AND NOT EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds h WHERE h.wallet_id=w.id AND h.status='open')
		  AND NOT EXISTS (SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.wallet_id=w.id
		                  AND a.status IN ('claimed','resource_ready','signing','broadcast','held'))
		  AND EXISTS (
		      SELECT 1 FROM reg_crypto_sweep_attempts a
		      JOIN reg_crypto_balance_balances b ON b.wallet_id=a.wallet_id AND b.token_id=a.token_id
		      JOIN cat_tokens t ON t.id=a.token_id
		      LEFT JOIN LATERAL (
		          SELECT p.merchant_id FROM doc_crypto_payments p
		          WHERE p.wallet_id=w.id AND p.token_id=a.token_id AND p.status IN ('confirmed','settled')
		          ORDER BY p.confirmed_at DESC NULLS LAST,p.created_at DESC LIMIT 1
		      ) pm ON TRUE
		      LEFT JOIN reg_merchant_token_config mtc ON mtc.merchant_id=COALESCE(w.merchant_id,pm.merchant_id) AND mtc.token_id=a.token_id
		      WHERE a.wallet_id=w.id AND a.status IN ('expired','failed')
		        AND a.updated_at > COALESCE(w.last_swept_at,'-infinity'::timestamptz)
		        AND b.amount > COALESCE(mtc.sweep_retain_amount,t.sweep_retain_amount,0)
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_sweep_attempts a
		      JOIN reg_crypto_signing_authorizations s ON s.basis_type='CryptoSweepAttempt' AND s.basis_id=a.id
		      WHERE a.wallet_id=w.id AND a.status IN ('expired','failed') AND s.status <> 'failed'
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM doc_crypto_native_fundings f
		      WHERE f.destination_wallet_id=w.id AND f.status NOT IN ('settled','failed')
		  )
	`, batch.Wallets)
	return err
}

func (r *OutboundRepo) MarkSweepAttemptResourceReady(ctx context.Context, attemptID id.ID) error {
	return r.updateSweepAttemptStatus(ctx, attemptID, sweepAttemptStatusResourceReady, "", "")
}

func (r *OutboundRepo) MarkSweepAttemptSigning(ctx context.Context, attemptID id.ID) error {
	return r.updateSweepAttemptStatus(ctx, attemptID, sweepAttemptStatusSigning, "", "")
}

func (r *OutboundRepo) MarkSweepAttemptBroadcast(ctx context.Context, attemptID id.ID) error {
	return r.updateSweepAttemptStatus(ctx, attemptID, sweepAttemptStatusBroadcast, "broadcast_at", "")
}

func (r *OutboundRepo) MarkSweepAttemptHeld(ctx context.Context, attemptID id.ID, reason string) error {
	return r.updateSweepAttemptStatus(ctx, attemptID, sweepAttemptStatusHeld, "", statusFailedReason(fmt.Errorf("%s", reason)))
}

func (r *OutboundRepo) MarkSweepAttemptFailed(ctx context.Context, attemptID id.ID, reason string) error {
	return r.updateSweepAttemptStatus(ctx, attemptID, sweepAttemptStatusFailed, "", statusFailedReason(fmt.Errorf("%s", reason)))
}

func (r *OutboundRepo) MarkSweepAttemptsPosted(ctx context.Context, attemptIDs []id.ID) error {
	if len(attemptIDs) == 0 {
		return nil
	}
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	_, err := querier.Exec(ctx, `
		UPDATE reg_crypto_sweep_attempts
		SET status = $2,
		    posted_at = COALESCE(posted_at, NOW()),
		    error_message = '',
		    updated_at = NOW()
		WHERE id = ANY($1)
		  AND status IN ($3, $2)
	`, attemptIDs, sweepAttemptStatusPosted, sweepAttemptStatusBroadcast)
	if err != nil {
		return fmt.Errorf("mark sweep attempts posted: %w", err)
	}
	return nil
}

func (r *OutboundRepo) updateSweepAttemptStatus(ctx context.Context, attemptID id.ID, status, timestampColumn, reason string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	switch timestampColumn {
	case "broadcast_at":
		_, err := querier.Exec(ctx, `
			UPDATE reg_crypto_sweep_attempts
			SET status = $2,
			    broadcast_at = COALESCE(broadcast_at, NOW()),
			    error_message = $3,
			    updated_at = NOW()
			WHERE id = $1 AND status NOT IN ('posted','failed','expired')
		`, attemptID, status, reason)
		if err != nil {
			return fmt.Errorf("update sweep attempt status: %w", err)
		}
	default:
		_, err := querier.Exec(ctx, `
			UPDATE reg_crypto_sweep_attempts
			SET status = $2,
			    error_message = $3,
			    updated_at = NOW()
			WHERE id = $1 AND status NOT IN ('posted','failed','expired')
		`, attemptID, status, reason)
		if err != nil {
			return fmt.Errorf("update sweep attempt status: %w", err)
		}
	}
	return nil
}

func scanSweepTaskRows(rows pgx.Rows, capacity int) ([]SweepTask, error) {
	tasks := make([]SweepTask, 0, capacity)
	for rows.Next() {
		var task SweepTask
		if err := rows.Scan(
			&task.AttemptID,
			&task.SweepID,
			&task.WalletID,
			&task.TokenID,
			&task.NetworkID,
			&task.NetworkCode,
			&task.Address,
			&task.VaultKeyID,
			&task.ContractAddress,
			&task.TokenStandard,
			&task.TokenDecimals,
			&task.ChainActivationStatus,
			&task.Balance,
			&task.RequiredConfs,
			&task.HotWalletID,
			&task.HotWalletAddress,
			&task.HotWalletVaultKeyID,
			&task.HotWalletBalance,
			&task.RecoveryOnly,
		); err != nil {
			return nil, fmt.Errorf("scan sweep task: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}
