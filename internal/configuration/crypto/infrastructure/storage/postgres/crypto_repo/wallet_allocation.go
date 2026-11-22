package crypto_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

const _walletAllocationsTable = "reg_crypto_wallet_allocations"

// WalletAllocationRepo persists transient wallet allocations.
type WalletAllocationRepo struct{}

func NewWalletAllocationRepo() *WalletAllocationRepo {
	return &WalletAllocationRepo{}
}

var _ crypto.WalletAllocationRepository = (*WalletAllocationRepo)(nil)
var _ crypto.LeaseCloser = (*WalletAllocationRepo)(nil)

func (r *WalletAllocationRepo) Open(ctx context.Context, alloc crypto.WalletAllocation) error {
	if alloc.ID == (id.ID{}) {
		alloc.ID = id.New()
	}
	if alloc.LeasedAt.IsZero() {
		alloc.LeasedAt = time.Now().UTC()
	}
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err := querier.Exec(ctx, `
		INSERT INTO `+_walletAllocationsTable+` (
		    id, wallet_id, invoice_id, merchant_id, token_id, network_id,
		    leased_at, closed_at, cooldown_until, close_reason, version
		) VALUES ($1,$2,$3,$4,$5,$6,$7,NULL,NULL,'',1)
	`, alloc.ID, alloc.WalletID, alloc.InvoiceID, alloc.MerchantID, alloc.TokenID, alloc.NetworkID, alloc.LeasedAt)
	if err != nil {
		return fmt.Errorf("open wallet allocation: %w", err)
	}
	return nil
}

func (r *WalletAllocationRepo) CloseOpenForWallet(
	ctx context.Context,
	walletID id.ID,
	reason crypto.AllocationCloseReason,
	closedAt, cooldownUntil time.Time,
) (*crypto.WalletAllocation, error) {
	if !reason.Valid() {
		return nil, apperror.NewValidation("invalid allocation close reason").WithDetail("reason", string(reason))
	}
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var alloc crypto.WalletAllocation
	err := pgxscan.Get(ctx, querier, &alloc, `
		UPDATE `+_walletAllocationsTable+`
		SET closed_at = $2,
		    cooldown_until = $3,
		    close_reason = $4,
		    version = version + 1
		WHERE wallet_id = $1
		  AND closed_at IS NULL
		  AND _deleted_at IS NULL
		RETURNING id, wallet_id, invoice_id, merchant_id, token_id, network_id,
		          leased_at, closed_at, cooldown_until, close_reason, version, created_at, updated_at
	`, walletID, closedAt.UTC(), cooldownUntil.UTC(), reason)
	if err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("close wallet allocation: %w", err)
	}
	return &alloc, nil
}

func (r *WalletAllocationRepo) GetOpenByWallet(ctx context.Context, walletID id.ID) (*crypto.WalletAllocation, error) {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var alloc crypto.WalletAllocation
	err := pgxscan.Get(ctx, querier, &alloc, `
		SELECT id, wallet_id, invoice_id, merchant_id, token_id, network_id,
		       leased_at, closed_at, cooldown_until, close_reason, version, created_at, updated_at
		FROM `+_walletAllocationsTable+`
		WHERE wallet_id = $1
		  AND closed_at IS NULL
		  AND _deleted_at IS NULL
	`, walletID)
	if err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get open wallet allocation: %w", err)
	}
	return &alloc, nil
}

func (r *WalletAllocationRepo) FindCovering(ctx context.Context, walletID id.ID, ts time.Time) ([]crypto.WalletAllocation, error) {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var rows []crypto.WalletAllocation
	err := pgxscan.Select(ctx, querier, &rows, `
		SELECT id, wallet_id, invoice_id, merchant_id, token_id, network_id,
		       leased_at, closed_at, cooldown_until, close_reason, version, created_at, updated_at
		FROM `+_walletAllocationsTable+`
		WHERE wallet_id = $1
		  AND _deleted_at IS NULL
		  AND (
		        (closed_at IS NOT NULL AND $2 >= leased_at AND $2 <= cooldown_until)
		     OR (closed_at IS NULL AND cooldown_until IS NULL AND $2 >= leased_at)
		      )
		ORDER BY leased_at
	`, walletID, ts.UTC())
	if err != nil {
		return nil, fmt.Errorf("find covering wallet allocations: %w", err)
	}
	return rows, nil
}

// CloseTransientLease closes the open allocation and moves the wallet off the current lease.
func (r *WalletAllocationRepo) CloseTransientLease(
	ctx context.Context,
	walletID id.ID,
	reason crypto.AllocationCloseReason,
	nextStatus wallet.WalletStatus,
) error {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	var (
		networkID      id.ID
		allocationMode wallet.AllocationMode
	)
	err := querier.QueryRow(ctx, `
		SELECT network_id, allocation_mode
		FROM cat_wallets
		WHERE id = $1
		FOR UPDATE
	`, walletID).Scan(&networkID, &allocationMode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewNotFound("wallet", walletID.String())
		}
		return fmt.Errorf("lock wallet for lease close: %w", err)
	}
	if allocationMode != wallet.AllocationModeTransient {
		return nil
	}

	var cooldownSeconds int
	if err := querier.QueryRow(ctx, `
		SELECT address_reuse_cooldown_seconds
		FROM sys_crypto_wallet_pool_policies
		WHERE network_id = $1 AND _deleted_at IS NULL
	`, networkID).Scan(&cooldownSeconds); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load reuse cooldown: %w", err)
	}

	now := time.Now().UTC()
	cooldownUntil := now.Add(time.Duration(cooldownSeconds) * time.Second)
	if _, err := r.CloseOpenForWallet(ctx, walletID, reason, now, cooldownUntil); err != nil {
		return err
	}

	tag, err := querier.Exec(ctx, `
		UPDATE cat_wallets
		SET status = $2,
		    leased_for_id = NULL,
		    leased_until = NULL,
		    cooldown_until = $3,
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $1
	`, walletID, nextStatus, cooldownUntil)
	if err != nil {
		return fmt.Errorf("update wallet after lease close: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewNotFound("wallet", walletID.String())
	}
	return nil
}
