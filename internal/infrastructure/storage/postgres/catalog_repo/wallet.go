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
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/infrastructure/storage/postgres"
)

const _walletTable = "cat_wallets"

// WalletRepo implements wallet.Repository.
type WalletRepo struct {
	*BaseCatalogRepo[*wallet.Wallet]
}

// NewWalletRepo creates a new wallet repository.
func NewWalletRepo() *WalletRepo {
	return &WalletRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*wallet.Wallet](
			_walletTable,
			postgres.ExtractDBColumns[wallet.Wallet](),
			func() *wallet.Wallet { return &wallet.Wallet{} },
			false, // flat catalog
		),
	}
}

// LeaseForInvoice atomically leases a free pool wallet for an invoice.
// Uses SELECT ... FOR UPDATE SKIP LOCKED for contention-free allocation.
func (r *WalletRepo) LeaseForInvoice(ctx context.Context, invoiceID, networkID id.ID) (*wallet.Wallet, error) {
	// Step 1: Find and lock a free pool wallet
	findSQL := `
		SELECT id FROM cat_wallets
		WHERE network_id = $1
		  AND status = $2
		  AND tier = $3
		  AND allocation_mode = 'transient'
		  AND is_active = true
		  AND deletion_mark = false
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	querier := r.getTxManager(ctx).GetQuerier(ctx)

	var walletID id.ID
	err := querier.QueryRow(ctx, findSQL, networkID, wallet.WalletStatusFree, wallet.WalletTierPool).Scan(&walletID)
	if err != nil {
		return nil, apperror.NewNotFound("free_wallet", networkID.String())
	}

	// Step 2: Update the wallet to leased state
	now := time.Now().UTC()
	leasedUntil := now.Add(30 * time.Minute) // default lease TTL

	// Use explicit column list — RETURNING * would include CDC columns
	// (created_at, updated_at) that don't map to BaseCatalog fields.
	returningCols := strings.Join(postgres.ExtractDBColumns[wallet.Wallet](), ", ")
	updateSQL := fmt.Sprintf(`
		UPDATE cat_wallets
		SET status = $1, leased_for_id = $2, leased_until = $3, version = version + 1
		WHERE id = $4
		RETURNING %s
	`, returningCols)

	var w wallet.Wallet
	if err := pgxscan.Get(ctx, querier, &w, updateSQL, wallet.WalletStatusLeased, invoiceID, leasedUntil, walletID); err != nil {
		return nil, fmt.Errorf("update wallet lease: %w", err)
	}

	return &w, nil
}

// FindByAddress retrieves a wallet by blockchain address and network.
func (r *WalletRepo) FindByAddress(ctx context.Context, networkID id.ID, address string) (*wallet.Wallet, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"network_id": networkID, "address": address}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

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

	return &w, nil
}

// CountFreeByNetwork returns the number of free pool wallets for a network.
func (r *WalletRepo) CountFreeByNetwork(ctx context.Context, networkID id.ID) (int, error) {
	q := r.Builder().Select("COUNT(*)").
		From(_walletTable).
		Where(squirrel.Eq{
			"network_id":    networkID,
			"status":        wallet.WalletStatusFree,
			"tier":          wallet.WalletTierPool,
			"is_active":     true,
			"deletion_mark": false,
		})

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
