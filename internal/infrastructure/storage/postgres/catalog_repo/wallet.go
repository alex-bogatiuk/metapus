package catalog_repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/infrastructure/storage/postgres"
)

// WalletRepo implements wallet.Repository.
type WalletRepo struct {
	*BaseCatalogRepo[*wallet.Wallet]
}

// NewWalletRepo creates a new wallet repository.
func NewWalletRepo() *WalletRepo {
	return &WalletRepo{
		BaseCatalogRepo: NewCatalogRepoForDefinition[*wallet.Wallet](wallet.Definition,
			postgres.ExtractDBColumns[wallet.Wallet](),
			func() *wallet.Wallet { return &wallet.Wallet{} }),
	}
}

// GetByID returns the catalog aggregate with its operational hold projection.
// Holds deliberately remain outside cat_wallets so independent incident
// workflows cannot overwrite or release one another.
func (r *WalletRepo) GetByID(ctx context.Context, entityID id.ID) (*wallet.Wallet, error) {
	item, err := r.BaseCatalogRepo.GetByID(ctx, entityID)
	if err != nil {
		return item, err
	}
	return item, r.loadAllocationHolds(ctx, []*wallet.Wallet{item})
}

func (r *WalletRepo) GetByCode(ctx context.Context, code string) (*wallet.Wallet, error) {
	item, err := r.BaseCatalogRepo.GetByCode(ctx, code)
	if err != nil {
		return item, err
	}
	return item, r.loadAllocationHolds(ctx, []*wallet.Wallet{item})
}

func (r *WalletRepo) List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*wallet.Wallet], error) {
	result, err := r.BaseCatalogRepo.List(ctx, filter)
	if err != nil {
		return result, err
	}
	if err := r.loadAllocationHolds(ctx, result.Items); err != nil {
		return domain.CursorListResult[*wallet.Wallet]{}, err
	}
	return result, nil
}

func (r *WalletRepo) loadAllocationHolds(ctx context.Context, items []*wallet.Wallet) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]id.ID, 0, len(items))
	byID := make(map[id.ID]*wallet.Wallet, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		item.AllocationHeld = false
		item.AllocationHoldReasons = nil
		ids = append(ids, item.ID)
		byID[item.ID] = item
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := r.getTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT wallet_id, array_agg(reason ORDER BY held_at, id)
		FROM reg_crypto_wallet_allocation_holds
		WHERE wallet_id = ANY($1) AND status = 'open'
		GROUP BY wallet_id
	`, ids)
	if err != nil {
		return fmt.Errorf("load wallet allocation holds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var walletID id.ID
		var reasons []string
		if err := rows.Scan(&walletID, &reasons); err != nil {
			return fmt.Errorf("scan wallet allocation holds: %w", err)
		}
		if item := byID[walletID]; item != nil {
			item.AllocationHeld = true
			item.AllocationHoldReasons = reasons
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate wallet allocation holds: %w", err)
	}
	return nil
}

// LeaseForInvoice atomically leases a free pool wallet for an invoice.
// Uses SELECT ... FOR UPDATE SKIP LOCKED for contention-free allocation.
func (r *WalletRepo) LeaseForInvoice(
	ctx context.Context,
	merchantID, invoiceID, networkID, tokenID id.ID,
	expiresAt time.Time,
) (*wallet.Wallet, error) {
	leasedUntil := expiresAt.UTC()
	if !leasedUntil.After(time.Now().UTC()) {
		return nil, apperror.NewValidation("invoice expiration must be in the future").
			WithDetail("expiresAt", leasedUntil)
	}
	if tokenID == (id.ID{}) {
		return nil, apperror.NewValidation("tokenId is required to lease a wallet")
	}

	var leased *wallet.Wallet
	txm := r.getTxManager(ctx)
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		policy, err := r.lockEffectiveAllocationPolicy(ctx, merchantID, networkID)
		if err != nil {
			return err
		}

		active, retryAfter, err := r.countActiveInvoiceLeases(ctx, merchantID, networkID, policy.RetryAfterSeconds)
		if err != nil {
			return err
		}
		if active >= policy.MaxActiveInvoiceLeases {
			return wallet.NewMerchantWalletLeaseLimitExceeded(networkID.String(), retryAfter)
		}

		ready, err := r.countReadyForAllocation(ctx, merchantID, networkID, policy.CustodyEnforced)
		if err != nil {
			return err
		}
		if ready == 0 || (!policy.CustodyEnforced && ready <= policy.MinGlobalReadyReserve) {
			return apperror.NewWalletPoolExhausted(networkID.String(), policy.RetryAfterSeconds)
		}

		rateRetryAfter, err := r.consumeLeaseRateWindow(ctx, merchantID, networkID, policy.MaxLeaseStartsPerMinute)
		if err != nil {
			return err
		}
		if rateRetryAfter > 0 {
			return wallet.NewMerchantWalletLeaseLimitExceeded(networkID.String(), rateRetryAfter)
		}

		leased, err = r.leaseReadyWallet(ctx, merchantID, invoiceID, networkID, leasedUntil, policy.RetryAfterSeconds, policy.CustodyEnforced)
		return err
	})
	if err != nil {
		return nil, err
	}
	return leased, nil
}

func (r *WalletRepo) leaseReadyWallet(
	ctx context.Context,
	merchantID, invoiceID, networkID id.ID,
	leasedUntil time.Time,
	retryAfter int,
	custodyEnforced bool,
) (*wallet.Wallet, error) {
	// Use an explicit list because RETURNING * also includes CDC columns.
	returningCols := strings.Join(postgres.ExtractDBColumns[wallet.Wallet](), ", ")
	updateSQL := fmt.Sprintf(`
		UPDATE cat_wallets
		SET status = $1, leased_for_id = $2, leased_until = $3, cooldown_until = NULL, version = version + 1
		WHERE id = (
			SELECT id FROM cat_wallets
			WHERE network_id = $4
			  AND (custody_merchant_id=$8 OR (NOT $9 AND custody_merchant_id IS NULL))
			  AND status = $5
			  AND tier = $6
			  AND wallet_role = 'merchant_pool'
			  AND chain_activation_status = $7
			  AND NOT EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds h
			                  WHERE h.wallet_id=cat_wallets.id AND h.status='open')
			  AND NOT EXISTS (
			      SELECT 1
			      FROM reg_crypto_reconciliation_items ri
			      WHERE ri.wallet_id = cat_wallets.id
			        AND ri.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
			        AND ri.status IN ('open','acknowledged')
			  )
			  AND allocation_mode = 'transient'
			  AND is_active = true
			  AND deletion_mark = false
			  AND (cooldown_until IS NULL OR cooldown_until < NOW())
			ORDER BY id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING %s
	`, returningCols)

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var w wallet.Wallet

	if err := pgxscan.Get(ctx, querier, &w, updateSQL,
		wallet.WalletStatusLeased, invoiceID, leasedUntil,
		networkID, wallet.WalletStatusFree, wallet.WalletTierPool,
		wallet.ChainActivationActive,
		merchantID, custodyEnforced,
	); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewWalletPoolExhausted(networkID.String(), retryAfter)
		}
		return nil, fmt.Errorf("update wallet lease: %w", err)
	}

	return &w, nil
}

type effectiveWalletAllocationPolicy struct {
	CustodyEnforced         bool
	MaxActiveInvoiceLeases  int
	MaxPersistentAddresses  int
	MaxLeaseStartsPerMinute int
	MinGlobalReadyReserve   int
	RetryAfterSeconds       int
}

func (r *WalletRepo) lockEffectiveAllocationPolicy(
	ctx context.Context,
	merchantID, networkID id.ID,
) (effectiveWalletAllocationPolicy, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var policy effectiveWalletAllocationPolicy
	err := querier.QueryRow(ctx, `
		SELECT COALESCE(o.max_active_invoice_leases, p.max_active_invoice_leases_per_merchant),
		       COALESCE(o.max_persistent_addresses, p.max_persistent_addresses_per_merchant),
		       COALESCE(o.max_lease_starts_per_minute, p.max_lease_starts_per_minute_per_merchant),
		       p.min_global_ready_reserve,
		       p.retry_after_seconds,
		       EXISTS(SELECT 1 FROM sys_crypto_custody_policies cp WHERE cp.network_id=p.network_id AND cp.enforced)
		FROM sys_crypto_wallet_pool_policies p
		JOIN sys_crypto_network_policies np
		  ON np.network_id=p.network_id
		 AND np._deleted_at IS NULL
		LEFT JOIN reg_merchant_wallet_allocation_overrides o
		  ON o.network_id = p.network_id
		 AND o.merchant_id = $1
		 AND o._deleted_at IS NULL
		WHERE p.network_id = $2
		  AND p._deleted_at IS NULL
		  AND p.enabled = TRUE
		  AND p.needs_review = FALSE
		  AND np.enabled = TRUE
		  AND p.lease_blocked_reason = ''
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_network_holds h
		      WHERE h.network_id=p.network_id AND h.status='open'
		  )
		FOR UPDATE OF p
	`, merchantID, networkID).Scan(
		&policy.MaxActiveInvoiceLeases,
		&policy.MaxPersistentAddresses,
		&policy.MaxLeaseStartsPerMinute,
		&policy.MinGlobalReadyReserve,
		&policy.RetryAfterSeconds,
		&policy.CustodyEnforced,
	)
	if err != nil {
		if pgxscan.NotFound(err) {
			return effectiveWalletAllocationPolicy{}, apperror.NewWalletPoolExhausted(networkID.String(), 30)
		}
		return effectiveWalletAllocationPolicy{}, fmt.Errorf("lock wallet allocation policy: %w", err)
	}
	return policy, nil
}

func (r *WalletRepo) countActiveInvoiceLeases(
	ctx context.Context,
	merchantID, networkID id.ID,
	defaultRetryAfter int,
) (int, int, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var count int
	var retryAfter int
	err := querier.QueryRow(ctx, `
		SELECT COUNT(*)::INT,
		       COALESCE(
		           GREATEST(1, CEIL(EXTRACT(EPOCH FROM MIN(w.leased_until) - NOW())))::INT,
		           $3
		       )
		FROM cat_wallets w
		JOIN doc_crypto_invoices i ON i.id = w.leased_for_id
		WHERE w.network_id = $1
		  AND i.merchant_id = $2
		  AND w.status = 'leased'
		  AND w.wallet_role = 'merchant_pool'
		  AND w.allocation_mode = 'transient'
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
	`, networkID, merchantID, defaultRetryAfter).Scan(&count, &retryAfter)
	if err != nil {
		return 0, 0, fmt.Errorf("count active merchant wallet leases: %w", err)
	}
	return count, retryAfter, nil
}

func (r *WalletRepo) countReadyForAllocation(ctx context.Context, merchantID, networkID id.ID, custodyEnforced bool) (int, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var count int
	err := querier.QueryRow(ctx, `
		SELECT COUNT(*)::INT
		FROM cat_wallets w
		WHERE w.network_id = $1
		  AND (w.custody_merchant_id=$2 OR (NOT $3 AND w.custody_merchant_id IS NULL))
		  AND w.status = 'free'
		  AND w.tier = 'pool'
		  AND w.wallet_role = 'merchant_pool'
		  AND w.chain_activation_status = 'active'
		  AND NOT EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds h
		                  WHERE h.wallet_id=w.id AND h.status='open')
		  AND NOT EXISTS (
		      SELECT 1
		      FROM reg_crypto_reconciliation_items ri
		      WHERE ri.wallet_id = w.id
		        AND ri.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
		        AND ri.status IN ('open','acknowledged')
		  )
		  AND w.allocation_mode = 'transient'
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
		  AND (w.cooldown_until IS NULL OR w.cooldown_until < NOW())
	`, networkID, merchantID, custodyEnforced).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count ready wallets for allocation: %w", err)
	}
	return count, nil
}

// consumeLeaseRateWindow returns zero when admission is allowed, otherwise
// the number of seconds until the fixed one-minute window resets.
func (r *WalletRepo) consumeLeaseRateWindow(
	ctx context.Context,
	merchantID, networkID id.ID,
	limit int,
) (int, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var starts, retryAfter int
	err := querier.QueryRow(ctx, `
		INSERT INTO reg_merchant_wallet_lease_rate_windows (
		    merchant_id, network_id, window_started_at, lease_starts
		) VALUES ($1, $2, NOW(), 1)
		ON CONFLICT (merchant_id, network_id) DO UPDATE
		SET window_started_at = CASE
		        WHEN reg_merchant_wallet_lease_rate_windows.window_started_at <= NOW() - INTERVAL '1 minute'
		        THEN NOW()
		        ELSE reg_merchant_wallet_lease_rate_windows.window_started_at
		    END,
		    lease_starts = CASE
		        WHEN reg_merchant_wallet_lease_rate_windows.window_started_at <= NOW() - INTERVAL '1 minute'
		        THEN 1
		        ELSE reg_merchant_wallet_lease_rate_windows.lease_starts + 1
		    END,
		    version = reg_merchant_wallet_lease_rate_windows.version + 1,
		    _deleted_at = NULL
		RETURNING lease_starts,
		          GREATEST(1, CEIL(EXTRACT(EPOCH FROM window_started_at + INTERVAL '1 minute' - NOW())))::INT
	`, merchantID, networkID).Scan(&starts, &retryAfter)
	if err != nil {
		return 0, fmt.Errorf("consume merchant wallet lease rate window: %w", err)
	}
	if starts > limit {
		return retryAfter, nil
	}
	return 0, nil
}

// FindByAddress retrieves a wallet by blockchain address and network.
func (r *WalletRepo) FindByAddress(ctx context.Context, networkID id.ID, address string) (*wallet.Wallet, error) {
	return r.findByAddress(ctx, networkID, address, false)
}

func (r *WalletRepo) FindByAddressForUpdate(ctx context.Context, networkID id.ID, address string) (*wallet.Wallet, error) {
	if r.getTxManager(ctx).GetTx(ctx) == nil {
		return nil, apperror.NewValidation("incoming wallet lock requires a transaction")
	}
	return r.findByAddress(ctx, networkID, address, true)
}

func (r *WalletRepo) findByAddress(ctx context.Context, networkID id.ID, address string, lock bool) (*wallet.Wallet, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"network_id": networkID, "address": address}).
		Where(squirrel.Expr("(deletion_mark=FALSE OR id IN (SELECT owner_id FROM sys_crypto_watch_targets WHERE network_id=? AND target_kind='address' AND target_key=?))", networkID, address)).
		OrderBy("CASE WHEN id=(SELECT owner_id FROM sys_crypto_watch_targets WHERE network_id=cat_wallets.network_id AND target_kind='address' AND target_key=cat_wallets.address) THEN 0 ELSE 1 END", "deletion_mark ASC", "created_at ASC").
		Limit(1)
	if lock {
		q = q.Suffix("FOR UPDATE")
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var w wallet.Wallet
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &w, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("wallet", address)
		}
		return nil, fmt.Errorf("find by address: %w", err)
	}
	return &w, r.loadAllocationHolds(ctx, []*wallet.Wallet{&w})
}

// CountFreeByNetwork returns the number of free pool wallets for a network.
func (r *WalletRepo) CountFreeByNetwork(ctx context.Context, networkID id.ID) (int, error) {
	q := r.Builder().Select("COUNT(*)").
		From(wallet.Definition.Table).
		Where(squirrel.Eq{
			"network_id":              networkID,
			"status":                  wallet.WalletStatusFree,
			"tier":                    wallet.WalletTierPool,
			"wallet_role":             wallet.WalletRoleMerchantPool,
			"chain_activation_status": wallet.ChainActivationActive,
			"is_active":               true,
			"deletion_mark":           false,
		}).
		Where(`NOT EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds h
		                  WHERE h.wallet_id=cat_wallets.id AND h.status='open')`).
		Where(`NOT EXISTS (
			SELECT 1
			FROM reg_crypto_reconciliation_items ri
			WHERE ri.wallet_id = cat_wallets.id
			  AND ri.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
			  AND ri.status IN ('open','acknowledged')
		)`).
		Where("(cooldown_until IS NULL OR cooldown_until < NOW())")

	sql, args, err := q.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var count int
	if err := querier.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count free wallets: %w", err)
	}

	return count, nil
}

// FindPersistentByCustomerID finds an existing persistent wallet for a customer on a specific network.
func (r *WalletRepo) FindPersistentByCustomerID(ctx context.Context, merchantID, networkID id.ID, customerID string) (*wallet.Wallet, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{
			"merchant_id":     merchantID,
			"network_id":      networkID,
			"customer_id":     customerID,
			"allocation_mode": wallet.AllocationModePersistent,
			"deletion_mark":   false,
		}).
		Where("deposit_retired_at IS NULL").
		Where(squirrel.Expr("(custody_merchant_id=? OR (custody_merchant_id IS NULL AND NOT EXISTS (SELECT 1 FROM sys_crypto_custody_policies cp WHERE cp.network_id=cat_wallets.network_id AND cp.enforced)))", merchantID)).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var w wallet.Wallet
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &w, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("persistent_wallet", customerID)
		}
		return nil, fmt.Errorf("find persistent by customer_id: %w", err)
	}
	return &w, r.loadAllocationHolds(ctx, []*wallet.Wallet{&w})
}

// AssignPersistentAddress atomically assigns a free pool wallet to a customer persistently.
//
// The network policy row serializes admission, so quota, reserve, idempotency
// re-check and allocation are one transaction.
func (r *WalletRepo) AssignPersistentAddress(ctx context.Context, merchantID, networkID id.ID, customerID string) (*wallet.Wallet, error) {
	var assigned *wallet.Wallet
	txm := r.getTxManager(ctx)
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		policy, err := r.lockEffectiveAllocationPolicy(ctx, merchantID, networkID)
		if err != nil {
			return err
		}

		// Re-check after taking the network admission lock. Concurrent replays
		// return the first assignment without consuming quota or reserve.
		existing, err := r.FindPersistentByCustomerID(ctx, merchantID, networkID, customerID)
		if err == nil {
			assigned = existing
			return nil
		}
		if appErr, ok := apperror.AsAppError(err); !ok || appErr.Code != apperror.CodeNotFound {
			return err
		}

		var persistentCount int
		querier := r.getTxManager(ctx).GetQuerier(ctx)
		if err := querier.QueryRow(ctx, `
			SELECT COUNT(*)::INT
			FROM cat_wallets
			WHERE merchant_id = $1
			  AND network_id = $2
			  AND allocation_mode = 'persistent'
			  AND deposit_retired_at IS NULL
			  AND is_active = TRUE
			  AND deletion_mark = FALSE
		`, merchantID, networkID).Scan(&persistentCount); err != nil {
			return fmt.Errorf("count persistent merchant wallets: %w", err)
		}
		if persistentCount >= policy.MaxPersistentAddresses {
			return wallet.NewMerchantPersistentAddressLimitExceeded(networkID.String())
		}

		ready, err := r.countReadyForAllocation(ctx, merchantID, networkID, policy.CustodyEnforced)
		if err != nil {
			return err
		}
		if ready == 0 || (!policy.CustodyEnforced && ready <= policy.MinGlobalReadyReserve) {
			return apperror.NewWalletPoolExhausted(networkID.String(), policy.RetryAfterSeconds)
		}

		retryAfter, err := r.consumeLeaseRateWindow(ctx, merchantID, networkID, policy.MaxLeaseStartsPerMinute)
		if err != nil {
			return err
		}
		if retryAfter > 0 {
			return wallet.NewMerchantWalletLeaseLimitExceeded(networkID.String(), retryAfter)
		}
		assigned, err = r.assignReadyPersistentWallet(ctx, merchantID, networkID, customerID, policy.RetryAfterSeconds, policy.CustodyEnforced, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return assigned, nil
}

func (r *WalletRepo) assignReadyPersistentWallet(
	ctx context.Context,
	merchantID, networkID id.ID,
	customerID string,
	retryAfter int,
	custodyEnforced bool,
	freshOnly bool,
) (*wallet.Wallet, error) {
	returningCols := strings.Join(postgres.ExtractDBColumns[wallet.Wallet](), ", ")

	updateSQL := fmt.Sprintf(`
		UPDATE cat_wallets
		SET status = $1, allocation_mode = $2, merchant_id = $3, customer_id = $4,
		    deposit_assigned_at = NOW(), version = version + 1
		WHERE id = (
			SELECT id FROM cat_wallets
			WHERE network_id = $5
			  AND (custody_merchant_id=$3 OR (NOT $9 AND custody_merchant_id IS NULL))
			  AND status = $6
			  AND tier = $7
			  AND wallet_role = 'merchant_pool'
			  AND chain_activation_status = $8
			  AND NOT EXISTS (SELECT 1 FROM reg_crypto_wallet_allocation_holds h
			                  WHERE h.wallet_id=cat_wallets.id AND h.status='open')
			  AND NOT EXISTS (
			      SELECT 1
			      FROM reg_crypto_reconciliation_items ri
			      WHERE ri.wallet_id = cat_wallets.id
			        AND ri.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
			        AND ri.status IN ('open','acknowledged')
			  )
			  AND allocation_mode = 'transient'
			  AND deposit_retired_at IS NULL
			  AND (NOT $10 OR (
			      NOT EXISTS(SELECT 1 FROM doc_crypto_payments p WHERE p.wallet_id=cat_wallets.id)
			      AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects e WHERE e.wallet_id=cat_wallets.id AND e.source_wallet_id IS NULL)
			      AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states risk WHERE risk.wallet_id=cat_wallets.id AND risk.risk_class='high')
			  ))
			  AND is_active = true
			  AND deletion_mark = false
			  AND (cooldown_until IS NULL OR cooldown_until < NOW())
			ORDER BY id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING %s
	`, returningCols)

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var w wallet.Wallet

	err := pgxscan.Get(ctx, querier, &w, updateSQL,
		wallet.WalletStatusAssigned, wallet.AllocationModePersistent, merchantID, customerID,
		networkID, wallet.WalletStatusFree, wallet.WalletTierPool,
		wallet.ChainActivationActive,
		custodyEnforced,
		freshOnly,
	)

	if err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewWalletPoolExhausted(networkID.String(), retryAfter)
		}
		return nil, fmt.Errorf("update wallet to persistent: %w", err)
	}

	return &w, nil
}

func (r *WalletRepo) BeginSweep(ctx context.Context, walletID id.ID) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE cat_wallets
		SET status = $2,
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $1
		  AND status IN ($3, $4)
		  AND deposit_retired_at IS NULL
		  AND deletion_mark = FALSE
	`, walletID, wallet.WalletStatusSweepPending, wallet.WalletStatusFree, wallet.WalletStatusAssigned)
	if err != nil {
		return fmt.Errorf("begin sweep: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewValidation("wallet is not reusable for sweep").
			WithDetail("walletId", walletID.String())
	}
	return nil
}
