package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/eventlog"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	defaultProvisioningLease = 2 * time.Minute
	maxProvisioningClaim     = 50
)

// WalletPoolRepo persists wallet-pool control-plane state.
type WalletPoolRepo struct{}

func NewWalletPoolRepo() *WalletPoolRepo {
	return &WalletPoolRepo{}
}

func (r *WalletPoolRepo) ListStates(ctx context.Context) ([]crypto.WalletPoolState, error) {
	txm := postgres.MustGetTxManager(ctx)
	rows, err := txm.GetQuerier(ctx).Query(ctx, walletPoolStateQuery+walletPoolStateGroupBy+`
		ORDER BY n.code, n.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list wallet pool states: %w", err)
	}
	defer rows.Close()

	states := make([]crypto.WalletPoolState, 0, 8)
	for rows.Next() {
		state, scanErr := scanWalletPoolState(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *WalletPoolRepo) GetState(ctx context.Context, networkID id.ID) (crypto.WalletPoolState, error) {
	txm := postgres.MustGetTxManager(ctx)
	row := txm.GetQuerier(ctx).QueryRow(ctx, walletPoolStateQuery+`
		AND p.network_id = $1
	`+walletPoolStateGroupBy, networkID)
	state, err := scanWalletPoolState(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return crypto.WalletPoolState{}, apperror.NewNotFound("wallet_pool_policy", networkID.String())
	}
	return state, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWalletPoolState(row rowScanner) (crypto.WalletPoolState, error) {
	var state crypto.WalletPoolState
	var maxConcurrent pgtype.Int4
	var activationReserve, minHotBalance, dailyActivationBudget postgres.NullableCryptoAmount
	err := row.Scan(
		&state.Policy.NetworkID,
		&state.Policy.NetworkCode,
		&state.Policy.NetworkName,
		&state.Policy.ChainFamily,
		&state.Policy.NativeTokenSymbol,
		&state.Policy.NativeDecimals,
		&state.Policy.Enabled,
		&state.Policy.LowWatermark,
		&state.Policy.TargetReady,
		&state.Policy.MaxTotalWallets,
		&state.Policy.MaxProvisionBatch,
		&maxConcurrent,
		&state.Policy.MaxActiveInvoiceLeasesPerMerchant,
		&state.Policy.MaxPersistentAddressesPerMerchant,
		&state.Policy.MaxLeaseStartsPerMinutePerMerchant,
		&state.Policy.MinGlobalReadyReserve,
		&activationReserve,
		&minHotBalance,
		&dailyActivationBudget,
		&state.Policy.RetryAfterSeconds,
		&state.Policy.AddressReuseCooldownSeconds,
		&state.Policy.MaxWatcherLagSeconds,
		&state.Policy.LeaseBlockedReason,
		&state.Policy.LeaseBlockedAt,
		&state.Policy.DerivationNamespace,
		&state.Policy.NeedsReview,
		&state.Policy.Version,
		&state.Policy.UpdatedAt,
		&state.Policy.UpdatedBy,
		&state.Policy.LastReconciledAt,
		&state.Policy.LastErrorCode,
		&state.Policy.LastErrorMessage,
		&state.Stats.Ready,
		&state.Stats.Cooldown,
		&state.Stats.Leased,
		&state.Stats.Assigned,
		&state.Stats.Provisioning,
		&state.Stats.Activating,
		&state.Stats.SweepPending,
		&state.Stats.Held,
		&state.Stats.Failed,
		&state.Stats.Total,
		&state.Stats.InboxUnmatched,
		&state.Stats.InboxNeedsReview,
		&state.Stats.InboxPoison,
	)
	if err != nil {
		return crypto.WalletPoolState{}, err
	}
	activationFields := []bool{maxConcurrent.Valid, activationReserve.Valid, minHotBalance.Valid, dailyActivationBudget.Valid}
	if activationFields[0] || activationFields[1] || activationFields[2] || activationFields[3] {
		if !activationFields[0] || !activationFields[1] || !activationFields[2] || !activationFields[3] {
			return crypto.WalletPoolState{}, errors.New("wallet pool activation policy is partially configured")
		}
		state.Policy.Activation = &crypto.WalletPoolActivationPolicy{
			MaxConcurrent:      int(maxConcurrent.Int32),
			ReserveMinor:       activationReserve.Amount,
			MinHotBalanceMinor: minHotBalance.Amount,
			DailyBudgetMinor:   dailyActivationBudget.Amount,
		}
	}
	state.State = crypto.ComputeWalletPoolState(state.Policy, state.Stats)
	return state, nil
}

const walletPoolStateQuery = `
	SELECT p.network_id,
	       n.code,
	       n.name,
	       n.chain_family,
	       n.native_token_symbol,
	       n.native_decimals,
	       p.enabled,
	       p.low_watermark,
	       p.target_ready,
	       p.max_total_wallets,
	       p.max_provision_batch,
	       p.max_concurrent_activations,
	       p.max_active_invoice_leases_per_merchant,
	       p.max_persistent_addresses_per_merchant,
	       p.max_lease_starts_per_minute_per_merchant,
	       p.min_global_ready_reserve,
	       p.activation_reserve_minor,
	       p.min_hot_native_balance_minor,
	       p.daily_activation_budget_minor,
	       p.retry_after_seconds,
	       p.address_reuse_cooldown_seconds,
	       p.max_watcher_lag_seconds,
	       p.lease_blocked_reason,
	       p.lease_blocked_at,
	       p.derivation_namespace,
	       p.needs_review,
	       p.version,
	       p.updated_at,
	       p.updated_by,
	       p.last_reconciled_at,
	       p.last_error_code,
	       p.last_error_message,
	       COUNT(*) FILTER (
	           WHERE w.status = 'free'
	             AND w.chain_activation_status = 'active'
	             AND NOT EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds wh
	                             WHERE wh.wallet_id=w.id AND wh.status='open')
	             AND w.allocation_mode = 'transient'
	             AND w.is_active = TRUE
	             AND w.deletion_mark = FALSE
	             AND NOT EXISTS (
	                 SELECT 1
	                 FROM reg_crypto_reconciliation_items ri
	                 WHERE ri.wallet_id = w.id
	                   AND ri.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
	                   AND ri.status IN ('open','acknowledged')
	             )
	             AND (w.cooldown_until IS NULL OR w.cooldown_until < NOW())
	       )::INT AS ready,
	       COUNT(*) FILTER (
	           WHERE w.status = 'free'
	             AND w.allocation_mode = 'transient'
	             AND w.is_active = TRUE
	             AND w.deletion_mark = FALSE
	             AND w.cooldown_until IS NOT NULL
	             AND w.cooldown_until >= NOW()
	       )::INT AS cooldown,
	       COUNT(*) FILTER (
	           WHERE w.status = 'leased'
	             AND w.is_active = TRUE
	             AND w.deletion_mark = FALSE
	       )::INT AS leased,
	       COUNT(*) FILTER (
	           WHERE w.status = 'assigned'
	             AND w.is_active = TRUE
	             AND w.deletion_mark = FALSE
	       )::INT AS assigned,
	       COALESCE(j.provisioning, 0)::INT AS provisioning,
	       COUNT(*) FILTER (
	           WHERE w.chain_activation_status IN ('unknown','activating')
	             AND w.is_active = TRUE
	             AND w.deletion_mark = FALSE
	       )::INT AS activating,
	       COUNT(*) FILTER (
	           WHERE w.status = 'sweep_pending'
	             AND w.is_active = TRUE
	             AND w.deletion_mark = FALSE
	       )::INT AS sweep_pending,
	       (
	           COUNT(*) FILTER (
	               WHERE (EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds wh
	                              WHERE wh.wallet_id=w.id AND wh.status='open')
	                      OR w.chain_activation_status = 'held')
	                 AND w.is_active = TRUE
	                 AND w.deletion_mark = FALSE
	           ) + COALESCE(j.held, 0)
	       )::INT AS held,
	       COUNT(*) FILTER (
	           WHERE w.chain_activation_status = 'failed'
	             AND w.is_active = TRUE
	             AND w.deletion_mark = FALSE
	       )::INT AS failed,
	       (
	           COUNT(*) FILTER (
	               WHERE w.tier = 'pool'
	                 AND w.is_active = TRUE
	                 AND w.deletion_mark = FALSE
	           ) + COALESCE(j.not_materialized, 0)
	       )::INT AS total,
	       COALESCE(inbox.unmatched, 0)::INT AS inbox_unmatched,
	       COALESCE(inbox.needs_review, 0)::INT AS inbox_needs_review,
	       COALESCE(inbox.poison, 0)::INT AS inbox_poison
	FROM sys_crypto_wallet_pool_policies p
	JOIN cat_blockchain_networks n
	  ON n.id = p.network_id
	 AND n.deletion_mark = FALSE
	LEFT JOIN cat_wallets w
	  ON w.network_id = p.network_id
	 AND w.tier = 'pool'
	 AND w.wallet_role = 'merchant_pool'
	LEFT JOIN LATERAL (
	    SELECT COUNT(*) FILTER (
	               WHERE status IN ('pending','processing','retry_wait','held')
	           ) AS provisioning,
	           COUNT(*) FILTER (WHERE status = 'held') AS held,
	           COUNT(*) FILTER (
	               WHERE wallet_id IS NULL AND status <> 'completed'
	           ) AS not_materialized
	    FROM sys_crypto_wallet_provisioning_jobs pj
	    WHERE pj.network_id = p.network_id
	) j ON TRUE
	LEFT JOIN LATERAL (
	    SELECT COUNT(*) FILTER (WHERE match_status = 'unmatched')::INT AS unmatched,
	           COUNT(*) FILTER (WHERE match_status = 'needs_review')::INT AS needs_review,
	           COUNT(*) FILTER (WHERE match_status = 'poison')::INT AS poison
	    FROM reg_crypto_inbound_chain_events e
	    WHERE e.network_id = p.network_id
	      AND e._deleted_at IS NULL
	) inbox ON TRUE
	WHERE TRUE
`

const walletPoolStateGroupBy = `
	GROUP BY p.network_id, n.id, n.code, n.name, n.chain_family, n.native_token_symbol, n.native_decimals,
	         p.enabled, p.low_watermark, p.target_ready, p.max_total_wallets,
	         p.max_provision_batch, p.max_concurrent_activations,
	         p.max_active_invoice_leases_per_merchant,
	         p.max_persistent_addresses_per_merchant,
	         p.max_lease_starts_per_minute_per_merchant,
	         p.min_global_ready_reserve,
	         p.activation_reserve_minor, p.min_hot_native_balance_minor,
	         p.daily_activation_budget_minor, p.retry_after_seconds,
	         p.address_reuse_cooldown_seconds, p.max_watcher_lag_seconds,
	         p.lease_blocked_reason, p.lease_blocked_at,
	         p.derivation_namespace, p.needs_review, p.version, p.updated_at,
	         p.updated_by, p.last_reconciled_at,
	         p.last_error_code, p.last_error_message,
	         j.provisioning, j.held, j.not_materialized,
	         inbox.unmatched, inbox.needs_review, inbox.poison
`

func (r *WalletPoolRepo) UpdatePolicy(
	ctx context.Context,
	policy crypto.WalletPoolPolicy,
	updatedBy string,
) (crypto.WalletPoolState, error) {
	var maxConcurrent, activationReserve, minHotBalance, dailyActivationBudget any
	if policy.Activation != nil {
		maxConcurrent = policy.Activation.MaxConcurrent
		activationReserve = policy.Activation.ReserveMinor
		minHotBalance = policy.Activation.MinHotBalanceMinor
		dailyActivationBudget = policy.Activation.DailyBudgetMinor
	}
	txm := postgres.MustGetTxManager(ctx)
	tag, err := txm.GetQuerier(ctx).Exec(ctx, `
		UPDATE sys_crypto_wallet_pool_policies
		SET enabled = $2,
		    low_watermark = $3,
		    target_ready = $4,
		    max_total_wallets = $5,
		    max_provision_batch = $6,
		    max_concurrent_activations = $7,
		    max_active_invoice_leases_per_merchant = $8,
		    max_persistent_addresses_per_merchant = $9,
		    max_lease_starts_per_minute_per_merchant = $10,
		    min_global_ready_reserve = $11,
		    activation_reserve_minor = $12,
		    min_hot_native_balance_minor = $13,
		    daily_activation_budget_minor = $14,
		    retry_after_seconds = $15,
		    address_reuse_cooldown_seconds = $16,
		    max_watcher_lag_seconds = $17,
		    updated_by = $18,
		    version = version + 1
		WHERE network_id = $1
		  AND version = $19
	`, policy.NetworkID, policy.Enabled, policy.LowWatermark, policy.TargetReady,
		policy.MaxTotalWallets, policy.MaxProvisionBatch, maxConcurrent,
		policy.MaxActiveInvoiceLeasesPerMerchant, policy.MaxPersistentAddressesPerMerchant,
		policy.MaxLeaseStartsPerMinutePerMerchant, policy.MinGlobalReadyReserve,
		activationReserve, minHotBalance, dailyActivationBudget, policy.RetryAfterSeconds,
		policy.AddressReuseCooldownSeconds, policy.MaxWatcherLagSeconds,
		strings.TrimSpace(updatedBy), policy.Version)
	if err != nil {
		return crypto.WalletPoolState{}, fmt.Errorf("update wallet pool policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if scanErr := txm.GetQuerier(ctx).QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM sys_crypto_wallet_pool_policies WHERE network_id = $1)`,
			policy.NetworkID,
		).Scan(&exists); scanErr != nil {
			return crypto.WalletPoolState{}, fmt.Errorf("check wallet pool policy: %w", scanErr)
		}
		if !exists {
			return crypto.WalletPoolState{}, apperror.NewNotFound("wallet_pool_policy", policy.NetworkID.String())
		}
		return crypto.WalletPoolState{}, apperror.NewConcurrentModification("wallet_pool_policy", policy.NetworkID.String())
	}
	return r.GetState(ctx, policy.NetworkID)
}

func (r *WalletPoolRepo) SetWatcherLagLeaseBlock(ctx context.Context, networkID id.ID, blocked bool) error {
	txm := postgres.MustGetTxManager(ctx)
	if blocked {
		_, err := txm.GetQuerier(ctx).Exec(ctx, `
			UPDATE sys_crypto_wallet_pool_policies
			SET lease_blocked_reason = $2,
			    lease_blocked_at = NOW(),
			    updated_at = NOW()
			WHERE network_id = $1
			  AND _deleted_at IS NULL
			  AND lease_blocked_reason <> $2
		`, networkID, crypto.LeaseBlockedReasonWatcherLag)
		if err != nil {
			return fmt.Errorf("set watcher lag lease block: %w", err)
		}
		return nil
	}
	_, err := txm.GetQuerier(ctx).Exec(ctx, `
		UPDATE sys_crypto_wallet_pool_policies
		SET lease_blocked_reason = '',
		    lease_blocked_at = NULL,
		    updated_at = NOW()
		WHERE network_id = $1
		  AND _deleted_at IS NULL
		  AND lease_blocked_reason = $2
	`, networkID, crypto.LeaseBlockedReasonWatcherLag)
	if err != nil {
		return fmt.Errorf("clear watcher lag lease block: %w", err)
	}
	return nil
}

func (r *WalletPoolRepo) GetMerchantAllocationOverride(
	ctx context.Context,
	merchantID, networkID id.ID,
) (crypto.MerchantWalletAllocationOverride, bool, error) {
	txm := postgres.MustGetTxManager(ctx)
	var override crypto.MerchantWalletAllocationOverride
	err := txm.GetQuerier(ctx).QueryRow(ctx, `
		SELECT merchant_id, network_id, max_active_invoice_leases,
		       max_persistent_addresses, max_lease_starts_per_minute,
		       version, updated_at, updated_by
		FROM reg_merchant_wallet_allocation_overrides
		WHERE merchant_id = $1
		  AND network_id = $2
		  AND _deleted_at IS NULL
	`, merchantID, networkID).Scan(
		&override.MerchantID,
		&override.NetworkID,
		&override.MaxActiveInvoiceLeases,
		&override.MaxPersistentAddresses,
		&override.MaxLeaseStartsPerMinute,
		&override.Version,
		&override.UpdatedAt,
		&override.UpdatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var merchantExists bool
		if scanErr := txm.GetQuerier(ctx).QueryRow(ctx, `
			SELECT EXISTS (
			    SELECT 1 FROM cat_merchants
			    WHERE id = $1 AND deletion_mark = FALSE
			)
		`, merchantID).Scan(&merchantExists); scanErr != nil {
			return crypto.MerchantWalletAllocationOverride{}, false, fmt.Errorf("check merchant for wallet allocation override: %w", scanErr)
		}
		if !merchantExists {
			return crypto.MerchantWalletAllocationOverride{}, false, apperror.NewNotFound("merchant", merchantID.String())
		}
		return crypto.MerchantWalletAllocationOverride{
			MerchantID: merchantID,
			NetworkID:  networkID,
		}, false, nil
	}
	if err != nil {
		return crypto.MerchantWalletAllocationOverride{}, false, fmt.Errorf("get merchant wallet allocation override: %w", err)
	}
	return override, true, nil
}

func (r *WalletPoolRepo) UpsertMerchantAllocationOverride(
	ctx context.Context,
	override crypto.MerchantWalletAllocationOverride,
	updatedBy string,
) (crypto.MerchantWalletAllocationOverride, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	var lockedNetwork id.ID
	if err := querier.QueryRow(ctx, `
		SELECT network_id
		FROM sys_crypto_wallet_pool_policies
		WHERE network_id = $1
		  AND _deleted_at IS NULL
		FOR UPDATE
	`, override.NetworkID).Scan(&lockedNetwork); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return crypto.MerchantWalletAllocationOverride{}, apperror.NewNotFound(
				"wallet_pool_policy", override.NetworkID.String(),
			)
		}
		return crypto.MerchantWalletAllocationOverride{}, fmt.Errorf("lock wallet pool policy for merchant override: %w", err)
	}
	var merchantExists bool
	if err := querier.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM cat_merchants
		    WHERE id = $1 AND deletion_mark = FALSE
		)
	`, override.MerchantID).Scan(&merchantExists); err != nil {
		return crypto.MerchantWalletAllocationOverride{}, fmt.Errorf("check merchant for wallet allocation override: %w", err)
	}
	if !merchantExists {
		return crypto.MerchantWalletAllocationOverride{}, apperror.NewNotFound(
			"merchant", override.MerchantID.String(),
		)
	}

	var saved crypto.MerchantWalletAllocationOverride
	err := querier.QueryRow(ctx, `
		INSERT INTO reg_merchant_wallet_allocation_overrides (
		    merchant_id, network_id, max_active_invoice_leases,
		    max_persistent_addresses, max_lease_starts_per_minute, updated_by
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (merchant_id, network_id) DO UPDATE
		SET max_active_invoice_leases = EXCLUDED.max_active_invoice_leases,
		    max_persistent_addresses = EXCLUDED.max_persistent_addresses,
		    max_lease_starts_per_minute = EXCLUDED.max_lease_starts_per_minute,
		    updated_by = EXCLUDED.updated_by,
		    version = reg_merchant_wallet_allocation_overrides.version + 1
		WHERE reg_merchant_wallet_allocation_overrides.version = $7
		  AND reg_merchant_wallet_allocation_overrides._deleted_at IS NULL
		RETURNING merchant_id, network_id, max_active_invoice_leases,
		          max_persistent_addresses, max_lease_starts_per_minute,
		          version, updated_at, updated_by
	`, override.MerchantID, override.NetworkID, override.MaxActiveInvoiceLeases,
		override.MaxPersistentAddresses, override.MaxLeaseStartsPerMinute,
		strings.TrimSpace(updatedBy), override.Version).Scan(
		&saved.MerchantID,
		&saved.NetworkID,
		&saved.MaxActiveInvoiceLeases,
		&saved.MaxPersistentAddresses,
		&saved.MaxLeaseStartsPerMinute,
		&saved.Version,
		&saved.UpdatedAt,
		&saved.UpdatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return crypto.MerchantWalletAllocationOverride{}, apperror.NewConcurrentModification(
			"merchant_wallet_allocation_override",
			override.MerchantID.String()+":"+override.NetworkID.String(),
		)
	}
	if err != nil {
		return crypto.MerchantWalletAllocationOverride{}, fmt.Errorf("upsert merchant wallet allocation override: %w", err)
	}
	return saved, nil
}

func (r *WalletPoolRepo) HasConfirmedNativeFunding(ctx context.Context, networkID id.ID) (bool, error) {
	txm := postgres.MustGetTxManager(ctx)
	var funded bool
	if err := txm.GetQuerier(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		    FROM reg_crypto_wallet_fundings f
		    JOIN cat_wallets w ON w.id = f.wallet_id
		    JOIN cat_tokens t ON t.id = f.token_id
		    WHERE f.network_id = $1
		      AND w.tier = 'hot'
		      AND w.wallet_role = 'liquidity_hot'
		      AND w.is_active = TRUE
		      AND w.deletion_mark = FALSE
		      AND t.token_standard = 'native'
		      AND t.is_active = TRUE
		      AND t.deletion_mark = FALSE
		)
	`, networkID).Scan(&funded); err != nil {
		return false, fmt.Errorf("read confirmed native wallet funding: %w", err)
	}
	return funded, nil
}

const walletPoolRefillCandidatesQuery = `
	SELECT p.network_id
	FROM sys_crypto_wallet_pool_policies p
	JOIN cat_blockchain_networks n ON n.id = p.network_id
	WHERE p.enabled = TRUE
	  AND p.needs_review = FALSE
	  AND p._deleted_at IS NULL
	  AND n.is_active = TRUE
	  AND n.deletion_mark = FALSE
	  AND NOT EXISTS (SELECT 1 FROM sys_crypto_custody_policies custody WHERE custody.network_id=p.network_id AND custody.enforced)
	  AND NOT EXISTS (
	      SELECT 1 FROM reg_crypto_network_holds h
	      WHERE h.network_id=p.network_id AND h.status='open'
	  )
	ORDER BY p.network_id
`

func (r *WalletPoolRepo) ListRefillCandidates(ctx context.Context) ([]id.ID, error) {
	txm := postgres.MustGetTxManager(ctx)
	rows, err := txm.GetQuerier(ctx).Query(ctx, walletPoolRefillCandidatesQuery)
	if err != nil {
		return nil, fmt.Errorf("list wallet pool refill candidates: %w", err)
	}
	defer rows.Close()

	networkIDs := make([]id.ID, 0, 8)
	for rows.Next() {
		var item id.ID
		if scanErr := rows.Scan(&item); scanErr != nil {
			return nil, fmt.Errorf("scan wallet pool refill candidate: %w", scanErr)
		}
		networkIDs = append(networkIDs, item)
	}
	return networkIDs, rows.Err()
}

func (r *WalletPoolRepo) PlanNetworkRefill(ctx context.Context, networkID id.ID) (crypto.WalletPoolPlanResult, error) {
	txm := postgres.MustGetTxManager(ctx)
	var result crypto.WalletPoolPlanResult
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		var planErr error
		result, planErr = r.planNetworkRefill(ctx, networkID)
		return planErr
	})
	return result, err
}

func (r *WalletPoolRepo) planNetworkRefill(ctx context.Context, networkID id.ID) (crypto.WalletPoolPlanResult, error) {
	result := crypto.WalletPoolPlanResult{NetworkID: networkID}
	txm := postgres.MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	var enabled, needsReview bool
	var low, target, maxTotal, maxBatch int
	var namespace string
	if err := q.QueryRow(ctx, `
		SELECT p.enabled, p.needs_review, p.low_watermark, p.target_ready,
		       max_total_wallets, max_provision_batch, derivation_namespace
		FROM sys_crypto_wallet_pool_policies p
		JOIN cat_blockchain_networks n ON n.id = p.network_id
		WHERE p.network_id = $1
		  AND p._deleted_at IS NULL
		  AND n.is_active = TRUE
		  AND n.deletion_mark = FALSE
		  AND p.lease_blocked_reason = ''
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_network_holds h
		      WHERE h.network_id=p.network_id AND h.status='open'
		  )
		FOR UPDATE OF p
	`, networkID).Scan(&enabled, &needsReview, &low, &target, &maxTotal, &maxBatch, &namespace); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, apperror.NewValidation("wallet pool network is inactive, deleted, or not configured")
		}
		return result, fmt.Errorf("lock wallet pool policy: %w", err)
	}
	if !enabled || needsReview {
		return result, nil
	}
	var custodyEnforced bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE network_id=$1 AND enforced)`, networkID).Scan(&custodyEnforced); err != nil {
		return result, err
	}
	if custodyEnforced {
		return result, nil
	}

	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (
		           WHERE status = 'free'
		             AND chain_activation_status = 'active'
		             AND NOT EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds wh
		                             WHERE wh.wallet_id=cat_wallets.id AND wh.status='open')
		             AND allocation_mode = 'transient'
		             AND (cooldown_until IS NULL OR cooldown_until < NOW())
		             AND NOT EXISTS (
		                 SELECT 1
		                 FROM reg_crypto_reconciliation_items ri
		                 WHERE ri.wallet_id = cat_wallets.id
		                   AND ri.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
		                   AND ri.status IN ('open','acknowledged')
		             )
		       )::INT,
		       COUNT(*) FILTER (
		           WHERE chain_activation_status IN ('unknown','activating','held')
		              OR EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds wh
		                         WHERE wh.wallet_id=cat_wallets.id AND wh.status='open'
		                           AND wh.reason='chain_balance_check_pending')
		       )::INT,
		       COUNT(*)::INT
		FROM cat_wallets
		WHERE network_id = $1
		  AND tier = 'pool'
		  AND wallet_role = 'merchant_pool'
		  AND is_active = TRUE
		  AND deletion_mark = FALSE
	`, networkID).Scan(&result.Ready, &result.InFlight, &result.Total); err != nil {
		return result, fmt.Errorf("count wallet pool capacity: %w", err)
	}

	var provisioning, notMaterialized int
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (
		           WHERE status IN ('pending','processing','retry_wait','held')
		       )::INT,
		       COUNT(*) FILTER (
		           WHERE wallet_id IS NULL AND status <> 'completed'
		       )::INT
		FROM sys_crypto_wallet_provisioning_jobs
		WHERE network_id = $1
	`, networkID).Scan(&provisioning, &notMaterialized); err != nil {
		return result, fmt.Errorf("count provisioning jobs: %w", err)
	}
	result.InFlight += provisioning
	result.Total += notMaterialized

	if result.Ready > low {
		return result, nil
	}
	deficit := target - result.Ready - result.InFlight
	capacity := maxTotal - result.Total
	toCreate := minInt(deficit, maxBatch, capacity)
	if toCreate <= 0 {
		return result, nil
	}

	var cursorNamespace string
	var nextIndex int64
	var cursorNeedsReview bool
	if err := q.QueryRow(ctx, `
		SELECT derivation_namespace, next_index, needs_review
		FROM sys_crypto_wallet_derivation_cursors
		WHERE network_id = $1
		FOR UPDATE
	`, networkID).Scan(&cursorNamespace, &nextIndex, &cursorNeedsReview); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, fmt.Errorf("wallet derivation cursor is not initialized for network %s", networkID)
		}
		return result, fmt.Errorf("lock wallet derivation cursor: %w", err)
	}
	if cursorNeedsReview || cursorNamespace == "" || cursorNamespace != namespace {
		return result, fmt.Errorf("wallet derivation cursor needs review for network %s", networkID)
	}

	for offset := range toCreate {
		path := fmt.Sprintf("%s/%d", cursorNamespace, nextIndex+int64(offset))
		jobID := id.New()
		if _, err := q.Exec(ctx, `
			INSERT INTO sys_crypto_wallet_derivations (
				network_id, derivation_path, source
			) VALUES ($1, $2, 'provisioning')
		`, networkID, path); err != nil {
			return result, fmt.Errorf("publish wallet derivation %s: %w", path, err)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO sys_crypto_wallet_provisioning_jobs (
				id, network_id, derivation_path, status, next_attempt_at
			) VALUES ($1, $2, $3, 'pending', NOW())
		`, jobID, networkID, path); err != nil {
			return result, fmt.Errorf("create provisioning job for %s: %w", path, err)
		}
	}
	if _, err := q.Exec(ctx, `
		UPDATE sys_crypto_wallet_derivation_cursors
		SET next_index = $2,
		    version = version + 1
		WHERE network_id = $1
	`, networkID, nextIndex+int64(toCreate)); err != nil {
		return result, fmt.Errorf("advance wallet derivation cursor: %w", err)
	}
	result.Created = toCreate
	return result, nil
}

func minInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func (r *WalletPoolRepo) ClaimProvisioningJobs(
	ctx context.Context,
	workerID string,
	limit int,
	lease time.Duration,
) ([]crypto.WalletProvisioningJob, error) {
	if limit <= 0 || limit > maxProvisioningClaim {
		limit = maxProvisioningClaim
	}
	if lease <= 0 {
		lease = defaultProvisioningLease
	}
	txm := postgres.MustGetTxManager(ctx)
	rows, err := txm.GetQuerier(ctx).Query(ctx, `
		WITH picked AS (
		    SELECT j.id
		    FROM sys_crypto_wallet_provisioning_jobs j
		    JOIN sys_crypto_wallet_pool_policies p ON p.network_id = j.network_id
		    WHERE p.enabled = TRUE
		      AND p.needs_review = FALSE
		      AND NOT EXISTS(SELECT 1 FROM sys_crypto_custody_policies custody WHERE custody.network_id=j.network_id AND custody.enforced)
		      AND (
		           (j.status IN ('pending','retry_wait') AND j.next_attempt_at <= NOW())
		        OR (j.status = 'processing' AND COALESCE(j.lease_expires_at, '-infinity'::timestamptz) <= NOW())
		      )
		    ORDER BY j.next_attempt_at, j.created_at, j.id
		    FOR UPDATE OF j SKIP LOCKED
		    LIMIT $1
		),
		claimed AS (
		    UPDATE sys_crypto_wallet_provisioning_jobs j
		    SET status = 'processing',
		        attempts = attempts + 1,
		        claimed_by = $2,
		        claim_token = gen_random_uuid_v7(),
		        claimed_at = NOW(),
		        lease_expires_at = NOW() + ($3 * interval '1 second'),
		        last_error = '',
		        version = version + 1
		    FROM picked
		    WHERE j.id = picked.id
		    RETURNING j.id, j.network_id, j.derivation_path, j.status,
		              j.attempts, j.next_attempt_at, j.wallet_id,
		              j.claimed_by, j.claim_token
		)
		SELECT c.id, c.network_id, n.code, c.derivation_path, c.status,
		       c.attempts, c.next_attempt_at, c.wallet_id,
		       c.claimed_by, c.claim_token
		FROM claimed c
		JOIN cat_blockchain_networks n ON n.id = c.network_id
		ORDER BY c.next_attempt_at, c.id
	`, limit, workerID, int64(lease.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("claim wallet provisioning jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]crypto.WalletProvisioningJob, 0, limit)
	for rows.Next() {
		var job crypto.WalletProvisioningJob
		if err := rows.Scan(
			&job.ID,
			&job.NetworkID,
			&job.NetworkCode,
			&job.DerivationPath,
			&job.Status,
			&job.Attempts,
			&job.NextAttemptAt,
			&job.WalletID,
			&job.ClaimedBy,
			&job.ClaimToken,
		); err != nil {
			return nil, fmt.Errorf("scan wallet provisioning job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *WalletPoolRepo) CompleteProvisioningJob(
	ctx context.Context,
	job crypto.WalletProvisioningJob,
	derivation crypto.WalletDerivation,
	mode crypto.WalletProvisioningMode,
) (id.ID, error) {
	if err := crypto.ValidateWalletDerivation(job, derivation); err != nil {
		return id.Nil(), err
	}
	accountStatus, err := initialChainActivationStatus(mode)
	if err != nil {
		return id.Nil(), err
	}
	txm := postgres.MustGetTxManager(ctx)
	var walletID id.ID
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var status crypto.WalletProvisioningJobStatus
		var existingWalletID *id.ID
		var claimedBy string
		var claimToken *id.ID
		if err := q.QueryRow(ctx, `
			SELECT status, wallet_id, claimed_by, claim_token
			FROM sys_crypto_wallet_provisioning_jobs
			WHERE id = $1
			FOR UPDATE
		`, job.ID).Scan(&status, &existingWalletID, &claimedBy, &claimToken); err != nil {
			return fmt.Errorf("lock provisioning job: %w", err)
		}
		if status == crypto.WalletProvisioningCompleted {
			if existingWalletID == nil {
				return fmt.Errorf("completed provisioning job %s has no wallet", job.ID)
			}
			walletID = *existingWalletID
			return nil
		}
		if status != crypto.WalletProvisioningProcessing {
			return fmt.Errorf("provisioning job %s is not processing", job.ID)
		}
		if claimToken == nil || *claimToken != job.ClaimToken || claimedBy != job.ClaimedBy {
			return fmt.Errorf("provisioning job %s fencing token is stale", job.ID)
		}

		code := walletCode(job.NetworkCode, job.DerivationPath)
		if err := q.QueryRow(ctx, `
			INSERT INTO cat_wallets (
				code, name, network_id, address, derivation_path, vault_key_id,
				tier, wallet_role, status, allocation_mode, is_active,
				chain_activation_status
			) VALUES (
				$1, $2, $3, $4, $5, $6,
				'pool', 'merchant_pool', 'free', 'transient', TRUE,
				$7
			)
			RETURNING id
		`, code, "Warm pool "+code, job.NetworkID, derivation.Address,
			derivation.DerivationPath, derivation.VaultKeyID, accountStatus).Scan(&walletID); err != nil {
			return fmt.Errorf("materialize provisioned wallet: %w", err)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO reg_crypto_wallet_allocation_holds (
				wallet_id,reason,owner_type,owner_id,details,held_by
			) VALUES ($1::uuid,'chain_balance_check_pending','wallet_reconciliation',$1::uuid::text,
			          jsonb_build_object('provisioningJobId',$2::text,'derivationPath',$3::text),'wallet-pool')
			ON CONFLICT (wallet_id,owner_type,owner_id) WHERE status='open' DO NOTHING
		`, walletID, job.ID, derivation.DerivationPath); err != nil {
			return fmt.Errorf("hold provisioned wallet pending reconciliation: %w", err)
		}

		var publishedWalletID *id.ID
		if err := q.QueryRow(ctx, `
			SELECT wallet_id
			FROM sys_crypto_wallet_derivations
			WHERE network_id = $1
			  AND derivation_path = $2
		`, job.NetworkID, job.DerivationPath).Scan(&publishedWalletID); err != nil {
			return fmt.Errorf("read published wallet derivation: %w", err)
		}
		if publishedWalletID == nil || *publishedWalletID != walletID {
			return fmt.Errorf("published wallet derivation identity conflict")
		}

		tag, err := q.Exec(ctx, `
			UPDATE sys_crypto_wallet_provisioning_jobs
			SET status = 'completed',
			    wallet_id = $2,
			    claimed_by = '',
			    claim_token = NULL,
			    lease_expires_at = NULL,
			    last_error = '',
			    completed_at = COALESCE(completed_at, NOW()),
			    version = version + 1
			WHERE id = $1
			  AND status = 'processing'
			  AND claimed_by = $3
			  AND claim_token = $4
		`, job.ID, walletID, job.ClaimedBy, job.ClaimToken)
		if err != nil {
			return fmt.Errorf("complete wallet provisioning job: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("wallet provisioning job lease was lost")
		}
		return nil
	})
	return walletID, err
}

func initialChainActivationStatus(mode crypto.WalletProvisioningMode) (wallet.ChainActivationStatus, error) {
	switch mode {
	case crypto.WalletProvisioningDerived:
		return wallet.ChainActivationActive, nil
	case crypto.WalletProvisioningDerivedWithActivation:
		return wallet.ChainActivationUnknown, nil
	default:
		return "", fmt.Errorf("wallet provisioning mode %q is not supported", mode)
	}
}

func walletCode(networkCode, derivationPath string) string {
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToUpper(networkCode))
	sum := sha256.Sum256([]byte(derivationPath))
	return "POOL-" + normalized + "-" + strings.ToUpper(hex.EncodeToString(sum[:6]))
}

func (r *WalletPoolRepo) RetryProvisioningJob(
	ctx context.Context,
	job crypto.WalletProvisioningJob,
	jobErr error,
	nextAttemptAt time.Time,
	held bool,
) error {
	status := crypto.WalletProvisioningRetryWait
	if held {
		status = crypto.WalletProvisioningHeld
	}
	message := ""
	if jobErr != nil {
		message = jobErr.Error()
	}
	if len(message) > 2000 {
		message = message[:2000]
	}
	txm := postgres.MustGetTxManager(ctx)
	tag, err := txm.GetQuerier(ctx).Exec(ctx, `
		UPDATE sys_crypto_wallet_provisioning_jobs
		SET status = $2,
		    next_attempt_at = $3,
		    claimed_by = '',
		    claim_token = NULL,
		    lease_expires_at = NULL,
		    last_error = $4,
		    version = version + 1
		WHERE id = $1
		  AND status = 'processing'
		  AND claimed_by = $5
		  AND claim_token = $6
	`, job.ID, status, nextAttemptAt.UTC(), message, job.ClaimedBy, job.ClaimToken)
	if err != nil {
		return fmt.Errorf("retry wallet provisioning job: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("wallet provisioning job lease was lost")
	}
	return nil
}

func (r *WalletPoolRepo) ListPendingWalletReconciliations(
	ctx context.Context,
	limit int,
) ([]crypto.WalletPendingReconciliation, error) {
	if limit <= 0 || limit > maxProvisioningClaim {
		limit = maxProvisioningClaim
	}
	txm := postgres.MustGetTxManager(ctx)
	rows, err := txm.GetQuerier(ctx).Query(ctx, `
		SELECT w.id, w.network_id
		FROM cat_wallets w
		JOIN sys_crypto_wallet_pool_policies p
		  ON p.network_id = w.network_id
		 AND p.enabled = TRUE
		 AND p.needs_review = FALSE
		WHERE ((w.tier = 'pool' AND w.wallet_role = 'merchant_pool') OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions provision WHERE provision.wallet_id=w.id AND provision.status='completed'))
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
		  AND EXISTS (
		      SELECT 1 FROM reg_crypto_wallet_allocation_holds wh
		      WHERE wh.wallet_id=w.id AND wh.status='open'
		        AND wh.reason='chain_balance_check_pending'
		  )
		ORDER BY w.updated_at, w.id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list wallets pending targeted reconciliation: %w", err)
	}
	defer rows.Close()

	items := make([]crypto.WalletPendingReconciliation, 0, limit)
	for rows.Next() {
		var item crypto.WalletPendingReconciliation
		if err := rows.Scan(&item.WalletID, &item.NetworkID); err != nil {
			return nil, fmt.Errorf("scan wallet pending targeted reconciliation: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *WalletPoolRepo) SetOperationalStatus(
	ctx context.Context,
	networkID id.ID,
	status crypto.WalletPoolOperationalStatus,
) error {
	code := strings.TrimSpace(status.ErrorCode)
	message := strings.TrimSpace(status.PublicMessage)
	if len(code) > 100 {
		code = code[:100]
	}
	if len(message) > 500 {
		message = message[:500]
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var previousCode string
		if err := q.QueryRow(ctx, `
			SELECT last_error_code
			FROM sys_crypto_wallet_pool_policies
			WHERE network_id = $1
			FOR UPDATE
		`, networkID).Scan(&previousCode); err != nil {
			return fmt.Errorf("lock wallet pool reconcile status: %w", err)
		}
		if _, err := q.Exec(ctx, `
			UPDATE sys_crypto_wallet_pool_policies
			SET last_reconciled_at = NOW(),
			    last_error_code = $2,
			    last_error_message = $3
			WHERE network_id = $1
		`, networkID, code, message); err != nil {
			return fmt.Errorf("update wallet pool reconcile status: %w", err)
		}
		if code == "" || code == previousCode {
			return nil
		}
		return postgres.NewEventLogRepo().Write(ctx, eventlog.Event{
			Category:   eventlog.CategoryBackground,
			Severity:   eventlog.SeverityError,
			EventType:  eventlog.EventType("crypto.wallet_pool.error"),
			Source:     "worker",
			EntityType: "blockchain_network",
			EntityID:   &networkID,
			Message:    "Wallet pool reconciliation is held by an external dependency or budget gate",
			Details: map[string]any{
				"networkId": networkID.String(),
				"errorCode": code,
			},
		})
	})
}

func (r *WalletPoolRepo) RecordStateTransition(ctx context.Context, state crypto.WalletPoolState) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		tag, err := q.Exec(ctx, `
			UPDATE sys_crypto_wallet_pool_policies
			SET last_emitted_state = $2
			WHERE network_id = $1
			  AND last_emitted_state IS DISTINCT FROM $2
		`, state.Policy.NetworkID, state.State)
		if err != nil {
			return fmt.Errorf("record wallet pool state: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}

		severity := eventlog.SeverityInfo
		switch state.State {
		case crypto.WalletPoolStateLow:
			severity = eventlog.SeverityWarning
		case crypto.WalletPoolStateDepleted:
			severity = eventlog.SeverityCritical
		}
		networkID := state.Policy.NetworkID
		return postgres.NewEventLogRepo().Write(ctx, eventlog.Event{
			Category:   eventlog.CategoryBackground,
			Severity:   severity,
			EventType:  eventlog.EventType("crypto.wallet_pool." + state.State),
			Source:     "worker",
			EntityType: "blockchain_network",
			EntityID:   &networkID,
			Message:    fmt.Sprintf("Wallet pool %s is %s", state.Policy.NetworkCode, state.State),
			Details: map[string]any{
				"networkId":    networkID.String(),
				"networkCode":  state.Policy.NetworkCode,
				"ready":        state.Stats.Ready,
				"targetReady":  state.Policy.TargetReady,
				"lowWatermark": state.Policy.LowWatermark,
			},
		})
	})
}

var (
	_ crypto.WalletPoolAdminRepository            = (*WalletPoolRepo)(nil)
	_ crypto.WalletPoolControllerRepository       = (*WalletPoolRepo)(nil)
	_ crypto.WalletPoolActivationStatusRepository = (*WalletPoolRepo)(nil)
)
