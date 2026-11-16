package outbound_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *OutboundRepo) EnsureSweepExecutionDocument(
	ctx context.Context,
	attemptIDs []id.ID,
	candidate *crypto_sweep.CryptoSweep,
	repo crypto_sweep.Repository,
) (*crypto_sweep.CryptoSweep, error) {
	if len(attemptIDs) == 0 {
		return nil, fmt.Errorf("sweep execution draft requires attempts")
	}
	txm := postgres.MustGetTxManager(ctx)
	var result *crypto_sweep.CryptoSweep
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := lockSweepDraftResources(txCtx, attemptIDs, candidate); err != nil {
			return err
		}
		q := txm.GetQuerier(txCtx)
		rows, err := q.Query(txCtx, `
			SELECT id,sweep_id
			FROM reg_crypto_sweep_attempts
			WHERE id=ANY($1)
			ORDER BY id
			FOR UPDATE
		`, attemptIDs)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := 0
		var existingID *id.ID
		for rows.Next() {
			var attemptID id.ID
			var sweepID *id.ID
			if err := rows.Scan(&attemptID, &sweepID); err != nil {
				return err
			}
			seen++
			if sweepID == nil {
				continue
			}
			if existingID != nil && *existingID != *sweepID {
				return fmt.Errorf("sweep attempts belong to different execution documents")
			}
			copyID := *sweepID
			existingID = &copyID
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if seen != len(attemptIDs) {
			return fmt.Errorf("one or more sweep attempts were not found")
		}
		if existingID == nil {
			if candidate == nil {
				return fmt.Errorf("new sweep execution draft requires a candidate")
			}
			if err := repo.Create(txCtx, candidate); err != nil {
				return fmt.Errorf("create sweep execution draft: %w", err)
			}
			if err := repo.SaveLines(txCtx, candidate.ID, candidate.GetLines()); err != nil {
				return fmt.Errorf("create sweep execution draft lines: %w", err)
			}
			existingID = &candidate.ID
		}
		tag, err := q.Exec(txCtx, `
			UPDATE reg_crypto_sweep_attempts
			SET sweep_id=$2,updated_at=NOW()
			WHERE id=ANY($1) AND (sweep_id IS NULL OR sweep_id=$2)
		`, attemptIDs, *existingID)
		if err != nil {
			return fmt.Errorf("link sweep execution draft: %w", err)
		}
		if int(tag.RowsAffected()) != len(attemptIDs) {
			return fmt.Errorf("sweep execution draft could not be linked to every attempt")
		}
		if candidate != nil && *existingID == candidate.ID {
			result = candidate
			return nil
		}
		existing, err := repo.GetByID(txCtx, *existingID)
		if err != nil {
			return err
		}
		lines, err := repo.GetLines(txCtx, *existingID)
		if err != nil {
			return err
		}
		existing.SetLines(lines)
		result = existing
		return nil
	})
	return result, err
}

func (r *OutboundRepo) RecordSweepLineBroadcast(ctx context.Context, sweepID, lineID id.ID, txHash string, amount types.RawAmount) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		tag, err := q.Exec(txCtx, `
			UPDATE doc_crypto_sweeps__lines
			SET tx_hash=$3,amount=$5,settlement_status=CASE WHEN tx_hash='' THEN $4 ELSE settlement_status END,settlement_error=CASE WHEN tx_hash='' THEN '' ELSE settlement_error END
			WHERE document_id=$1 AND line_id=$2 AND (tx_hash='' OR (tx_hash=$3 AND amount=$5))
		`, sweepID, lineID, txHash, outbound_settlement.StatusUnknown, amount)
		if err != nil {
			return fmt.Errorf("record sweep execution line broadcast: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("sweep execution line not found or has a different transaction")
		}
		_, err = q.Exec(txCtx, `
			UPDATE doc_crypto_sweeps
			SET status='broadcast',updated_at=NOW()
			WHERE id=$1 AND status IN ('created','signed','broadcast','held') AND posted=FALSE
		`, sweepID)
		return err
	})
}

func (r *OutboundRepo) MarkSweepExecutionStatus(ctx context.Context, sweepID id.ID, status string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err := q.Exec(ctx, `
		UPDATE doc_crypto_sweeps
		SET status=$2,updated_at=NOW()
		WHERE id=$1 AND posted=FALSE AND deletion_mark=FALSE
	`, sweepID, status)
	if err != nil {
		return fmt.Errorf("mark sweep execution status: %w", err)
	}
	return nil
}

func (r *OutboundRepo) GetSweepSigningAttempt(
	ctx context.Context,
	task SweepTask,
) (SigningAuthorizationRecord, bool, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var sourceWallet HotWallet
	var record SigningAuthorizationRecord
	err := r.scanSigningAttempt(querier.QueryRow(ctx, `
		SELECT w.id,
		       w.address,
		       w.vault_key_id,
		       COALESCE(b.amount, 0) AS balance,`+signingAttemptSelectColumns+`
		FROM reg_crypto_signing_authorizations a
		JOIN cat_wallets w
		  ON w.id = $2
		 AND w.address = a.source_address
		 AND w.vault_key_id = a.vault_key_id
		LEFT JOIN reg_crypto_balance_balances b
		  ON b.wallet_id = w.id AND b.token_id = $3
		WHERE a.purpose = $4
		  AND a.basis_type = $5
		  AND a.basis_id = $1
		  AND a.attempt = 1
		  AND a.status IN ('created','signed','held','broadcast','failed')
		ORDER BY a.created_at DESC
		LIMIT 1
	`, task.AttemptID, task.WalletID, task.TokenID, crypto.SigningPurposeSweep, _sweepAttemptBasisType), &sourceWallet, &record)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SigningAuthorizationRecord{}, false, nil
		}
		return SigningAuthorizationRecord{}, false, err
	}
	return record, true, nil
}

// Shared eligibility and collector ranking for planning and locked revalidation
// of direct sweeps: pools whose merchant/network/token has no enabled AML
// routing scope. AML-routed pools are planned as AML batch sweeps instead.
const directSweepCandidateCTEs = `picked AS (
			SELECT w.id
			FROM cat_wallets w
			WHERE w.status = $1
              AND ($13::uuid[] IS NULL OR w.id=ANY($13))
			  AND w.deposit_retired_at IS NULL
			  AND w.tier = $2
			  AND w.wallet_role = 'merchant_pool'
			  AND (w.custody_merchant_id IS NOT NULL OR NOT EXISTS(
			      SELECT 1 FROM sys_crypto_custody_policies cp WHERE cp.network_id=w.network_id AND cp.enforced))
              AND w.address<>'' AND w.vault_key_id<>''
              AND EXISTS(SELECT 1 FROM cat_wallets collector WHERE collector.network_id=w.network_id
                  AND collector.custody_merchant_id IS NOT DISTINCT FROM w.custody_merchant_id
                  AND collector.wallet_role='liquidity_hot' AND collector.tier=$10 AND collector.is_active AND NOT collector.deletion_mark
                  AND collector.address<>'' AND collector.vault_key_id<>'')
			  AND w.chain_activation_status = $12
			  AND w.is_active = TRUE
			  AND w.deletion_mark = FALSE
			  AND NOT EXISTS (
			      SELECT 1 FROM reg_crypto_sweep_attempts active
			      WHERE active.wallet_id = w.id
			        AND active.status IN ($5, $6, $7, $8, $9)
			  )
			  AND EXISTS (
			      SELECT 1
			      FROM reg_crypto_balance_balances b
			      JOIN cat_tokens t ON t.id = b.token_id
			      LEFT JOIN LATERAL (
			          SELECT p.merchant_id
			          FROM doc_crypto_payments p
			          WHERE p.wallet_id = w.id
			            AND p.token_id = b.token_id
			            AND p.status IN ('confirmed', 'settled')
			          ORDER BY p.confirmed_at DESC NULLS LAST, p.created_at DESC
			          LIMIT 1
			      ) pm ON TRUE
			      LEFT JOIN reg_merchant_token_config mtc
			        ON mtc.merchant_id = COALESCE(w.custody_merchant_id,w.merchant_id,pm.merchant_id)
			       AND mtc.token_id = b.token_id
			      WHERE b.wallet_id = w.id
                    AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes scope WHERE scope.merchant_id=w.custody_merchant_id AND scope.network_id=w.network_id AND scope.token_id=b.token_id AND scope.enabled)
                    AND b.amount > COALESCE(mtc.sweep_retain_amount, t.sweep_retain_amount, 0)
			        AND NOT EXISTS (
			            SELECT 1
			            FROM reg_crypto_sweep_attempts a
			            WHERE a.wallet_id = w.id
			              AND a.token_id = b.token_id
			              AND a.status IN ($5, $6, $7, $8, $9)
			        )
			  )
			ORDER BY COALESCE(w.last_swept_at, w.updated_at), w.id
		),
		candidates AS (
			SELECT w.id AS wallet_id,
			       b.token_id,
			       t.network_id,
			       COALESCE(n.code, '') AS network_code,
			       w.address AS source_address,
			       w.vault_key_id AS source_vault_key_id,
			       COALESCE(t.contract_address, '') AS contract_address,
			       t.token_standard,
			       w.chain_activation_status,
			       b.amount - COALESCE(mtc.sweep_retain_amount, t.sweep_retain_amount, 0) AS amount,
			       COALESCE(n.confirmations_needed, 1) AS required_confs,
			       hw.id AS hot_wallet_id,
			       hw.address AS hot_wallet_address,
			       hw.vault_key_id AS hot_wallet_vault_key_id,
			       COALESCE(hw.balance, 0) AS hot_wallet_balance
			FROM picked p
			JOIN cat_wallets w ON w.id = p.id
			JOIN reg_crypto_balance_balances b ON b.wallet_id = w.id
			JOIN cat_tokens t ON t.id = b.token_id
			JOIN cat_blockchain_networks n ON n.id = t.network_id
			LEFT JOIN LATERAL (
			    SELECT pmt.merchant_id
			    FROM doc_crypto_payments pmt
			    WHERE pmt.wallet_id = w.id
			      AND pmt.token_id = b.token_id
			      AND pmt.status IN ('confirmed', 'settled')
			    ORDER BY pmt.confirmed_at DESC NULLS LAST, pmt.created_at DESC
			    LIMIT 1
			) pm ON TRUE
			LEFT JOIN reg_merchant_token_config mtc
			  ON mtc.merchant_id = COALESCE(w.custody_merchant_id,w.merchant_id,pm.merchant_id)
			 AND mtc.token_id = b.token_id
			JOIN LATERAL (
			    SELECT h.id, h.address, h.vault_key_id, COALESCE(hb.amount, 0) AS balance
			    FROM cat_wallets h
			    LEFT JOIN reg_crypto_balance_balances hb
			      ON hb.wallet_id = h.id AND hb.token_id = b.token_id
			    WHERE h.network_id = t.network_id
                  AND ($14::uuid[] IS NULL OR h.id=ANY($14))
			      AND h.tier = $10
			      AND h.wallet_role = 'liquidity_hot'
			      AND h.custody_merchant_id IS NOT DISTINCT FROM w.custody_merchant_id
                  AND h.address<>'' AND h.vault_key_id<>''
			      AND h.is_active = TRUE
			      AND h.deletion_mark = FALSE
			    ORDER BY COALESCE(hb.amount, 0) DESC, h.code ASC
			    LIMIT 1
			) hw ON TRUE
			WHERE NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes scope WHERE scope.merchant_id=w.custody_merchant_id AND scope.network_id=w.network_id AND scope.token_id=b.token_id AND scope.enabled)
              AND b.amount > COALESCE(mtc.sweep_retain_amount, t.sweep_retain_amount, 0)
			  AND NOT EXISTS (
			      SELECT 1
			      FROM reg_crypto_sweep_attempts a
			      WHERE a.wallet_id = w.id
			        AND a.token_id = b.token_id
			        AND a.status IN ($5, $6, $7, $8, $9)
			  )
		),`

func (r *OutboundRepo) ClaimSweepPendingWallets(ctx context.Context, limit int) ([]SweepTask, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}
	var tasks []SweepTask
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		sources, resources, err := r.lockDirectSweepCandidates(ctx, limit)
		if err != nil || len(sources) == 0 {
			return err
		}
		tasks, err = r.claimLockedSweepWallets(ctx, limit, sources, resources)
		return err
	})
	return tasks, err
}

func (r *OutboundRepo) lockDirectSweepCandidates(ctx context.Context, limit int) ([]id.ID, []id.ID, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `WITH selector_parameters AS (SELECT $4::text,$11::bigint),`+directSweepCandidateCTEs+`
 footprints AS MATERIALIZED (
  SELECT c.wallet_id,array_prepend(c.wallet_id,array_agg(DISTINCT c.hot_wallet_id)) wallets
  FROM candidates c JOIN cat_wallets w ON w.id=c.wallet_id
  GROUP BY c.wallet_id,w.last_swept_at,w.updated_at
  ORDER BY COALESCE(w.last_swept_at,w.updated_at),c.wallet_id
 )
 SELECT wallet_id,wallets FROM footprints WHERE fn_crypto_try_lock_custody_wallets(wallets) LIMIT $3`,
		wallet.WalletStatusSweepPending, wallet.WalletTierPool, limit, wallet.WalletStatusSweeping,
		sweepAttemptStatusClaimed, sweepAttemptStatusResourceReady, sweepAttemptStatusSigning, sweepAttemptStatusBroadcast, sweepAttemptStatusHeld,
		wallet.WalletTierHot, int64(_signingAuthorizationTTL.Seconds()), wallet.ChainActivationActive, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var sources, resources []id.ID
	for rows.Next() {
		var source id.ID
		var footprint []id.ID
		if err := rows.Scan(&source, &footprint); err != nil {
			return nil, nil, err
		}
		sources = append(sources, source)
		resources = append(resources, footprint...)
	}
	return sources, resources, rows.Err()
}

func (r *OutboundRepo) claimLockedSweepWallets(ctx context.Context, limit int, sources, resources []id.ID) ([]SweepTask, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `WITH selector_parameters AS (SELECT $3::int),`+directSweepCandidateCTEs+`
		inserted AS (
			INSERT INTO reg_crypto_sweep_attempts (
				id, wallet_id, token_id, network_id, hot_wallet_id,
				source_address, destination_address, amount, status, claimed_at, expires_at
			)
			SELECT gen_random_uuid_v7(), wallet_id, token_id, network_id, hot_wallet_id,
			       source_address, hot_wallet_address, amount, $5, NOW(), NOW() + ($11 * interval '1 second')
			FROM candidates c
			-- Gas belongs to the token phase. Never create a native drain in
			-- the same wallet cycle, even when another worker runs concurrently.
			WHERE c.token_standard <> 'native' OR (
                NOT EXISTS(SELECT 1 FROM reg_crypto_balance_balances held_balance JOIN cat_tokens held_token ON held_token.id=held_balance.token_id
                    JOIN cat_wallets held_wallet ON held_wallet.id=held_balance.wallet_id
                    JOIN sys_crypto_aml_routing_scopes held_scope ON held_scope.merchant_id=held_wallet.custody_merchant_id AND held_scope.network_id=held_wallet.network_id AND held_scope.token_id=held_balance.token_id AND held_scope.enabled
                    WHERE held_balance.wallet_id=c.wallet_id AND held_balance.amount>0 AND held_token.token_standard<>'native') AND
			    NOT EXISTS (SELECT 1 FROM candidates sibling
			                WHERE sibling.wallet_id=c.wallet_id AND sibling.token_standard <> 'native')
			    AND NOT EXISTS (
			        SELECT 1 FROM reg_crypto_fee_reservations fee
			        WHERE fee.payer_wallet_id=c.wallet_id
			          AND fee.operation_token_id <> fee.native_token_id
			          AND fee.status IN ('reserved','broadcast','held')
			    )
			)
			ON CONFLICT DO NOTHING
			RETURNING id, wallet_id, token_id, network_id, hot_wallet_id,
			          source_address, destination_address, amount
		),
		updated AS (
			UPDATE cat_wallets w
			SET status = $4,
			    updated_at = NOW(),
			    version = version + 1
			FROM inserted i
			WHERE w.id = i.wallet_id
			  AND w.status = $1
			RETURNING w.id
		)
		SELECT i.id,
		       NULL::uuid AS sweep_id,
		       i.wallet_id,
		       i.token_id,
		       t.network_id,
		       COALESCE(n.code, '') AS network_code,
		       i.source_address,
		       w.vault_key_id,
		       COALESCE(t.contract_address, '') AS contract_address,
		       t.token_standard,
		       t.decimal_places,
		       w.chain_activation_status,
		       i.amount,
		       COALESCE(n.confirmations_needed, 1) AS required_confs,
		       i.hot_wallet_id,
		       i.destination_address,
		       hw.vault_key_id,
		       COALESCE(hb.amount, 0) AS hot_wallet_balance,FALSE AS recovery_only
		FROM inserted i
		JOIN updated u ON u.id = i.wallet_id
		JOIN cat_wallets w ON w.id = i.wallet_id
		JOIN cat_wallets hw ON hw.id = i.hot_wallet_id
		JOIN cat_tokens t ON t.id = i.token_id
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN reg_crypto_balance_balances hb
		  ON hb.wallet_id = hw.id AND hb.token_id = i.token_id
		ORDER BY t.network_id, i.token_id, i.amount DESC
	`, wallet.WalletStatusSweepPending, wallet.WalletTierPool, limit, wallet.WalletStatusSweeping,
		sweepAttemptStatusClaimed, sweepAttemptStatusResourceReady, sweepAttemptStatusSigning,
		sweepAttemptStatusBroadcast, sweepAttemptStatusHeld, wallet.WalletTierHot,
		int64(_signingAuthorizationTTL.Seconds()), wallet.ChainActivationActive, sources, resources)
	if err != nil {
		return nil, fmt.Errorf("claim sweep pending wallets: %w", err)
	}
	defer rows.Close()

	return scanSweepTaskRows(rows, limit)
}

// MarkWalletsFreeAfterSweep restores swept pool wallets to their reusable
// allocation state: transient wallets become free, while persistent wallets
// remain assigned to their customer.
func (r *OutboundRepo) MarkWalletsFreeAfterSweep(ctx context.Context, walletIDs []id.ID) error {
	if len(walletIDs) == 0 {
		return nil
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	_, err := querier.Exec(ctx, `
		UPDATE cat_wallets w
		SET status = CASE
		        WHEN allocation_mode = $4 THEN $5
		        ELSE $2
		    END,
		    leased_until = NULL,
		    leased_for_id = NULL,
		    last_swept_at = NOW(),
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = ANY($1)
		  AND status = $3
		  AND deletion_mark = FALSE
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_sweep_attempts a
		      WHERE a.wallet_id=w.id AND a.status IN ('claimed','resource_ready','signing','broadcast','held')
		  )
	`, walletIDs, wallet.WalletStatusFree, wallet.WalletStatusSweeping,
		wallet.AllocationModePersistent, wallet.WalletStatusAssigned)
	if err != nil {
		return fmt.Errorf("mark wallets free after sweep: %w", err)
	}
	return nil
}

// ReleaseSweepClaims returns claimed wallets to sweep_pending when execution
// failed before any on-chain broadcast.
func (r *OutboundRepo) ReleaseSweepClaims(ctx context.Context, walletIDs []id.ID) error {
	if len(walletIDs) == 0 {
		return nil
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	_, err := querier.Exec(ctx, `
		UPDATE cat_wallets w
		SET status = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = ANY($1)
		  AND status = $3
		  AND deletion_mark = FALSE
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_sweep_attempts a
		      WHERE a.wallet_id=w.id AND a.status IN ('claimed','resource_ready','signing','broadcast','held')
		  )
	`, walletIDs, wallet.WalletStatusSweepPending, wallet.WalletStatusSweeping)
	if err != nil {
		return fmt.Errorf("release sweep claims: %w", err)
	}
	return nil
}
