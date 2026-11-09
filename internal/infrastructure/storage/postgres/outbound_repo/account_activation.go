package outbound_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	_accountActivationBasisType = crypto_network_operation.ActivationTaskBasisType
	_accountActivationPeriod    = outboundapp.ActivationRetryPeriod
	_activationCostRepairLease  = 5 * time.Minute
	_accountActivationLease     = 2 * time.Minute
)

var errActivationConcurrencyLimit = outboundapp.ErrActivationConcurrencyLimit

// AccountActivationTask is one pool wallet that must exist on-chain before use.
type AccountActivationTask = outboundapp.AccountActivationTask

type activationCostRepairTask = outboundapp.ActivationCostRepairTask

func (r *OutboundRepo) ClaimActivationCostRepairs(ctx context.Context, workerID string, limit int) ([]activationCostRepairTask, error) {
	if workerID == "" {
		return nil, apperror.NewValidation("activation cost repair requires a worker")
	}
	var tasks []activationCostRepairTask
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		var isolation string
		if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return err
		}
		if isolation != "read committed" {
			return apperror.NewValidation("activation cost repair requires read committed isolation")
		}
		var err error
		tasks, err = r.claimActivationCostRepairs(ctx, workerID, limit)
		return err
	})
	return tasks, err
}
func (r *OutboundRepo) claimActivationCostRepairs(ctx context.Context, workerID string, limit int) ([]activationCostRepairTask, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}
	txm := postgres.MustGetTxManager(ctx)
	rows, err := txm.GetQuerier(ctx).Query(ctx, `
		WITH picked AS (
			SELECT a.id
			FROM reg_crypto_account_activations a
			JOIN doc_crypto_network_operations operation ON operation.id=a.network_operation_id
			WHERE operation.tx_hash <> ''
			  AND a.status IN ('confirmed','active','failed')
			  AND (a.cost_lease_expires_at IS NULL OR a.cost_lease_expires_at<=clock_timestamp())
			  AND (
			        a.cost_status = 'unknown'
			     OR (a.cost_status = 'held' AND COALESCE(a.cost_checked_at, '-infinity'::timestamptz) < clock_timestamp() - ($3 * interval '1 second'))
			     OR (a.cost_status = 'repairing' AND COALESCE(a.cost_lease_expires_at, '-infinity'::timestamptz) < clock_timestamp())
			  )
			ORDER BY COALESCE(a.cost_checked_at, a.created_at), a.id
			FOR UPDATE OF a SKIP LOCKED
			LIMIT $2
		), claimed AS (
			UPDATE reg_crypto_account_activations a
			SET cost_status = 'repairing',
			    cost_claim_token=gen_random_uuid_v7(),
			    cost_claimed_by = $1,
			    cost_claimed_at = clock_timestamp(),
			    cost_lease_expires_at = clock_timestamp() + ($4 * interval '1 second'),
			    cost_error = '',
			    updated_at = clock_timestamp()
			FROM picked
			WHERE a.id = picked.id
			RETURNING a.id, a.network_id, a.network_operation_id,a.cost_claim_token
		)
		SELECT claimed.id, claimed.network_id, operation.tx_hash,claimed.cost_claim_token
		FROM claimed
		JOIN doc_crypto_network_operations operation ON operation.id=claimed.network_operation_id
		ORDER BY claimed.id
	`, workerID, limit, int64(_accountActivationPeriod.Seconds()), int64(_activationCostRepairLease.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("claim account activation cost repairs: %w", err)
	}
	defer rows.Close()

	tasks := make([]activationCostRepairTask, 0, limit)
	for rows.Next() {
		var task activationCostRepairTask
		if err := rows.Scan(&task.ActivationID, &task.NetworkID, &task.TxHash, &task.CostClaimToken); err != nil {
			return nil, fmt.Errorf("scan account activation cost repair: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// ReserveAccountActivationBudget atomically gates the first signing attempt. It
// intentionally trusts only the internal ledger, never an unregistered
// on-chain transfer, when deciding whether the hot wallet may spend TRX.
func (r *OutboundRepo) ReserveAccountActivationBudget(
	ctx context.Context,
	task AccountActivationTask,
) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransactionWithOptions(ctx, postgres.SerializableTxOptions(), func(ctx context.Context) error {
		if err := r.prepareAccountActivationState(ctx, task, true); err != nil {
			return err
		}
		q := txm.GetQuerier(ctx)

		var enabled, needsReview bool
		var maxConcurrent int
		var reserve, minHotBalance, dailyBudget types.CryptoAmount
		if err := q.QueryRow(ctx, `
			SELECT enabled,
			       needs_review,
			       max_concurrent_activations,
			       activation_reserve_minor,
			       min_hot_native_balance_minor,
			       daily_activation_budget_minor
			FROM sys_crypto_wallet_pool_policies
			WHERE network_id = $1
			FOR UPDATE
		`, task.NetworkID).Scan(
			&enabled,
			&needsReview,
			&maxConcurrent,
			&reserve,
			&minHotBalance,
			&dailyBudget,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("wallet activation policy is missing")
			}
			return fmt.Errorf("lock wallet activation policy: %w", err)
		}
		if !enabled || needsReview {
			return fmt.Errorf("wallet activation policy is disabled or needs review")
		}

		var existingReserve types.CryptoAmount
		var existingReservedAt *time.Time
		var status string
		if err := q.QueryRow(ctx, `
			SELECT activation_cost_reserved_minor, activation_cost_reserved_at, status
			FROM reg_crypto_account_activations
			WHERE id = $1
			FOR UPDATE
		`, task.ActivationID).Scan(&existingReserve, &existingReservedAt, &status); err != nil {
			return fmt.Errorf("lock account activation budget reservation: %w", err)
		}
		if existingReservedAt != nil {
			return nil
		}
		if status != "claimed" && status != "held" {
			return fmt.Errorf("account activation is not eligible for budget reservation")
		}

		var concurrent int
		var reservedTotal, spentToday, hotBalance types.CryptoAmount
		if err := q.QueryRow(ctx, `
			SELECT COUNT(*)::INT,
			       COALESCE(SUM(activation_cost_reserved_minor), 0)
			FROM reg_crypto_account_activations
			WHERE network_id = $1
			  AND id <> $2
			  AND activation_cost_reserved_at IS NOT NULL
			  AND status NOT IN ('active','failed')
		`, task.NetworkID, task.ActivationID).Scan(&concurrent, &reservedTotal); err != nil {
			return fmt.Errorf("read active activation reservations: %w", err)
		}
		if concurrent >= maxConcurrent {
			return fmt.Errorf(
				"%w: active=%d limit=%d",
				errActivationConcurrencyLimit,
				concurrent,
				maxConcurrent,
			)
		}

		if err := q.QueryRow(ctx, `
			SELECT COALESCE(SUM(amount), 0)
			FROM reg_crypto_platform_cost_movements
			WHERE network_id = $1
			  AND purpose = 'activation'
			  AND record_type = 'expense'
			  AND period >= date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
		`, task.NetworkID).Scan(&spentToday); err != nil {
			return fmt.Errorf("read daily activation spend: %w", err)
		}
		if spentToday.Add(reservedTotal).Add(reserve).Cmp(dailyBudget) > 0 {
			return fmt.Errorf(
				"daily activation budget exhausted: spent=%s reserved=%s requested=%s budget=%s",
				spentToday,
				reservedTotal,
				reserve,
				dailyBudget,
			)
		}

		if err := q.QueryRow(ctx, `
			SELECT COALESCE((
			    SELECT amount
			    FROM reg_crypto_balance_balances
			    WHERE wallet_id = $1 AND token_id = $2
			), 0)
		`, task.HotWallet.ID, task.CostTokenID).Scan(&hotBalance); err != nil {
			return fmt.Errorf("read hot wallet ledger balance: %w", err)
		}
		if hotBalance.Cmp(reservedTotal.Add(reserve).Add(minHotBalance)) < 0 {
			return fmt.Errorf(
				"hot wallet ledger reserve is insufficient: balance=%s reserved=%s requested=%s minimum=%s",
				hotBalance,
				reservedTotal,
				reserve,
				minHotBalance,
			)
		}

		tag, err := q.Exec(ctx, `
			UPDATE reg_crypto_account_activations
			SET activation_cost_reserved_minor = $2,
			    activation_cost_reserved_at = NOW(),
			    updated_at = NOW()
			WHERE id = $1
			  AND activation_cost_reserved_at IS NULL
			  AND status IN ('claimed','held')
		`, task.ActivationID, reserve)
		if err != nil {
			return fmt.Errorf("reserve activation budget: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("activation budget reservation lost a concurrent race")
		}
		return nil
	})
}

func (r *OutboundRepo) MarkAccountActivationSigning(ctx context.Context, activationID id.ID) error {
	return r.markAccountActivationSigning(ctx, activationID, nil)
}
func (r *OutboundRepo) markAccountActivationSigning(ctx context.Context, activationID id.ID, claim *AccountActivationTask) error {
	return mutateActivationCommandState(ctx, activationID, claim, func(ctx context.Context) error {
		txm := postgres.MustGetTxManager(ctx)
		querier := txm.GetQuerier(ctx)
		_, err := querier.Exec(ctx, `
		UPDATE reg_crypto_account_activations
		SET status = 'signing',
		    error_message = '',
		    updated_at = NOW()
		WHERE id = $1
		  AND status IN ('claimed','held')
	`, activationID)
		if err != nil {
			return fmt.Errorf("mark account activation signing: %w", err)
		}
		return nil
	})
}

func (r *OutboundRepo) MarkAccountActivationActive(ctx context.Context, activationID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockActivationState(ctx, activationID); err != nil {
			return err
		}
		querier := txm.GetQuerier(ctx)
		var walletID id.ID
		var status string
		var linked *id.ID
		if err := querier.QueryRow(ctx, `SELECT wallet_id,status,network_operation_id FROM reg_crypto_account_activations WHERE id=$1`, activationID).Scan(&walletID, &status, &linked); err != nil {
			return err
		}
		if status == "active" {
			return nil
		}
		if err := ensureActivationNotSuperseded(ctx, activationID, walletID); err != nil {
			return err
		}
		if err := querier.QueryRow(ctx, `
			UPDATE reg_crypto_account_activations
			SET status = 'active',
			    confirmed_at = COALESCE(confirmed_at, NOW()),
			    error_message = '',
			    activation_claimed_by = '',
			    activation_claim_token = NULL,
			    activation_lease_expires_at = NULL,
			    updated_at = NOW()
			WHERE id = $1
			RETURNING wallet_id
		`, activationID).Scan(&walletID); err != nil {
			return fmt.Errorf("mark account activation active: %w", err)
		}

		_, err := querier.Exec(ctx, `
			UPDATE cat_wallets
			SET chain_activation_status = $2,
			    updated_at = NOW(),
			    version = version + 1
			WHERE id = $1 AND chain_activation_status<>$2
		`, walletID, wallet.ChainActivationActive)
		if err != nil {
			return fmt.Errorf("mark wallet chain active: %w", err)
		}

		return nil
	})
}

func (r *OutboundRepo) MarkAccountActivationExternallyActive(ctx context.Context, activationID, _ id.ID) error {
	return r.markAccountActivationExternallyActive(ctx, activationID, nil)
}
func (r *OutboundRepo) markAccountActivationExternallyActive(ctx context.Context, activationID id.ID, claim *AccountActivationTask) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockActivationCommandState(ctx, activationID, claim); err != nil {
			return err
		}
		querier := txm.GetQuerier(ctx)
		var walletID id.ID
		var status string
		var linked *id.ID
		if err := querier.QueryRow(ctx, `SELECT wallet_id,status,network_operation_id FROM reg_crypto_account_activations WHERE id=$1`, activationID).Scan(&walletID, &status, &linked); err != nil {
			return err
		}
		if linked != nil {
			return apperror.NewConflict("external activation requires an unlinked task")
		}
		if status == "active" {
			return nil
		}
		exposed, err := r.HasAccountActivationSigningExposure(ctx, AccountActivationTask{ActivationID: activationID})
		if err != nil {
			return err
		}
		if exposed {
			return apperror.NewConflict("external activation requires no signing exposure")
		}
		if err := ensureActivationNotSuperseded(ctx, activationID, walletID); err != nil {
			return err
		}
		if err := querier.QueryRow(ctx, `
			UPDATE reg_crypto_account_activations
			SET status = 'active',
			    activation_cost_reserved_minor = 0,
			    activation_cost_reserved_at = NULL,
			    cost_status = 'not_applicable',
			    cost_checked_at = NOW(),
			    cost_error = '',
			    cost_claim_token=NULL,
			    cost_claimed_by = '',
			    cost_claimed_at = NULL,
			    cost_lease_expires_at = NULL,
			    confirmed_at = COALESCE(confirmed_at, NOW()),
			    error_message = '',
			    activation_claimed_by = '',
			    activation_claim_token = NULL,
			    activation_lease_expires_at = NULL,
			    updated_at = NOW()
			WHERE id = $1
			  AND network_operation_id IS NULL
			RETURNING wallet_id
		`, activationID).Scan(&walletID); err != nil {
			return fmt.Errorf("mark externally activated account active: %w", err)
		}
		_, err = querier.Exec(ctx, `
			UPDATE cat_wallets
			SET chain_activation_status = $2,
			    updated_at = NOW(),
			    version = version + 1
			WHERE id = $1 AND chain_activation_status<>$2
		`, walletID, wallet.ChainActivationActive)
		if err != nil {
			return fmt.Errorf("mark externally activated wallet chain active: %w", err)
		}
		return nil
	})
}

func (r *OutboundRepo) MarkAccountActivationHeld(ctx context.Context, activationID id.ID, reason string) error {
	return r.updateAccountActivationStatus(ctx, activationID, "held", wallet.ChainActivationHeld, reason, nil)
}

// DeferAccountActivation releases a claim that did not reach signing.
// It is used for normal concurrency backpressure and must never modify a task
// that already owns a budget reservation or transaction.
func (r *OutboundRepo) DeferAccountActivation(
	ctx context.Context,
	activationID id.ID,
	delay time.Duration,
) error {
	return r.deferAccountActivation(ctx, activationID, delay, nil)
}
func (r *OutboundRepo) deferAccountActivation(ctx context.Context, activationID id.ID, delay time.Duration, claim *AccountActivationTask) error {
	if delay < 0 {
		delay = 0
	}
	return mutateActivationCommandState(ctx, activationID, claim, func(ctx context.Context) error {
		txm := postgres.MustGetTxManager(ctx)
		tag, err := txm.GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_account_activations
		SET activation_next_attempt_at = clock_timestamp() + ($2 * interval '1 microsecond'),
		    activation_claimed_by = '',
		    activation_claim_token = NULL,
		    activation_lease_expires_at = NULL,
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'claimed'
		  AND network_operation_id IS NULL
		  AND activation_cost_reserved_at IS NULL
		  AND activation_claim_token IS NOT NULL
	`, activationID, delay.Microseconds())
		if err != nil {
			return fmt.Errorf("defer account activation: %w", err)
		}
		if tag.RowsAffected() != 1 {
			var released bool
			if err := txm.GetQuerier(ctx).QueryRow(ctx, `SELECT status='claimed' AND network_operation_id IS NULL AND activation_cost_reserved_at IS NULL AND activation_claim_token IS NULL FROM reg_crypto_account_activations WHERE id=$1`, activationID).Scan(&released); err != nil {
				return err
			}
			if !released {
				return fmt.Errorf("defer account activation: task is no longer safely claimable")
			}
		}
		return nil
	})
}

func (r *OutboundRepo) MarkAccountActivationFailed(ctx context.Context, activationID id.ID, reason string) error {
	return r.updateAccountActivationStatus(ctx, activationID, "failed", wallet.ChainActivationFailed, reason, nil)
}

func (r *OutboundRepo) updateAccountActivationStatus(ctx context.Context, activationID id.ID, activationStatus string, walletStatus wallet.ChainActivationStatus, reason string, claim *AccountActivationTask) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockActivationCommandState(ctx, activationID, claim); err != nil {
			return err
		}
		querier := txm.GetQuerier(ctx)
		var walletID id.ID
		var currentStatus, currentReason string
		if err := querier.QueryRow(ctx, `SELECT status,error_message FROM reg_crypto_account_activations WHERE id=$1`, activationID).Scan(&currentStatus, &currentReason); err != nil {
			return err
		}
		if currentStatus == "active" {
			return nil
		}
		if currentStatus == "failed" {
			if activationStatus == "held" {
				return nil
			}
			if currentReason != statusFailedReason(errors.New(reason)) {
				return apperror.NewConflict("activation failure replay differs from its original reason")
			}
			return nil
		}
		if activationStatus == "failed" {
			exposed, err := r.HasAccountActivationSigningExposure(ctx, AccountActivationTask{ActivationID: activationID})
			if err != nil {
				return err
			}
			if exposed {
				return apperror.NewConflict("activation failure requires reconciliation of its signing exposure")
			}
		}

		if err := querier.QueryRow(ctx, `
			UPDATE reg_crypto_account_activations
			SET status = $2,
			    error_message = $3,
			    activation_cost_reserved_minor = CASE
			        WHEN $2 = 'failed' THEN 0
			        ELSE activation_cost_reserved_minor
			    END,
			    activation_cost_reserved_at = CASE
			        WHEN $2 = 'failed' THEN NULL
			        ELSE activation_cost_reserved_at
			    END,
			    activation_next_attempt_at = CASE
			        WHEN $2 = 'held' THEN
			            clock_timestamp() + (
			                LEAST(
			                    300.0,
			                    power(2.0, LEAST(GREATEST(activation_attempts - 1, 0), 8))
			                ) * (1.0 + random() * 0.25)
			            ) * interval '1 second'
			        ELSE NOW()
			    END,
			    activation_claimed_by = '',
			    activation_claim_token = NULL,
			    activation_lease_expires_at = NULL,
			    updated_at = NOW()
			WHERE id = $1
			RETURNING wallet_id
		`, activationID, activationStatus, statusFailedReason(errors.New(reason))).Scan(&walletID); err != nil {
			return fmt.Errorf("update account activation: %w", err)
		}
		_, err := querier.Exec(ctx, `
			UPDATE cat_wallets
			SET chain_activation_status = $2,
			    updated_at = NOW(),
			    version = version + 1
			WHERE id = $1
			  AND chain_activation_status <> $3
		`, walletID, walletStatus, wallet.ChainActivationActive)
		if err != nil {
			return fmt.Errorf("update wallet chain activation status: %w", err)
		}
		return nil
	})
}

func (r *OutboundRepo) GetAccountActivationSigningAttempt(ctx context.Context, task AccountActivationTask) (SigningAuthorizationRecord, bool, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var hotWallet HotWallet
	var record SigningAuthorizationRecord
	err := r.scanSigningAttempt(querier.QueryRow(ctx, `
		SELECT h.id,
		       h.address,
		       h.vault_key_id,
		       COALESCE(b.amount, 0) AS balance,`+signingAttemptSelectColumns+`
		FROM reg_crypto_signing_authorizations a
		JOIN cat_wallets h
		  ON h.network_id = $2
		 AND h.address = a.source_address
		 AND h.vault_key_id = a.vault_key_id
		LEFT JOIN reg_crypto_balance_balances b
		  ON b.wallet_id = h.id AND b.token_id = $3
		WHERE a.purpose = $4
		  AND a.basis_type = $5
		  AND a.basis_id = $1
		  AND a.attempt = 1
		  AND a.status IN ('created','signed','held','broadcast')
		ORDER BY a.created_at DESC
		LIMIT 1
	`, task.ActivationID, task.NetworkID, task.CostTokenID, crypto.SigningPurposeActivation, _accountActivationBasisType), &hotWallet, &record)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SigningAuthorizationRecord{}, false, nil
		}
		return SigningAuthorizationRecord{}, false, err
	}
	record.SourceWallet = hotWallet
	return record, true, nil
}
