//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestPortalTransactionItemsIncludeExplorerURLTemplate(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	errRollback := errors.New("rollback portal explorer fixture")
	err = postgres.ReadSnapshot(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var tokenID id.ID
		var explorerURL string
		if err := q.QueryRow(txCtx, `
			SELECT t.id, n.explorer_url
			FROM cat_tokens t
			JOIN cat_blockchain_networks n ON n.id = t.network_id
			WHERE t.deletion_mark = FALSE AND n.deletion_mark = FALSE
				AND n.explorer_url <> ''
			ORDER BY t.created_at
			LIMIT 1
		`).Scan(&tokenID, &explorerURL); err != nil {
			t.Skip("test database has no token with an explorer URL template")
		}

		merchantID := id.New()
		invoiceID := id.New()
		withdrawalID := id.New()
		publicRef := "ci_" + strings.ReplaceAll(invoiceID.String(), "-", "")
		invoiceNumber := fmt.Sprintf("CI-IT-%s", id.Compact(invoiceID))
		withdrawalNumber := fmt.Sprintf("CW-IT-%s", id.Compact(withdrawalID))

		if _, err := q.Exec(txCtx, `
			INSERT INTO doc_crypto_invoices (
				id, public_ref, number, date, merchant_id, token_id,
				expected_amount, status, expires_at
			) VALUES ($1, $2, $3, NOW(), $4, $5, 1, 'created', $6)
		`, invoiceID, publicRef, invoiceNumber, merchantID, tokenID, time.Now().Add(time.Hour)); err != nil {
			return err
		}

		if _, err := q.Exec(txCtx, `
			INSERT INTO doc_crypto_withdrawals (
				id, number, merchant_id, token_id, source_wallet_id,
				dest_address, amount, tx_hash, status
			) VALUES ($1, $2, $3, $4, $5, 'TIntegrationDestination', 1, $6, 'confirmed')
		`, withdrawalID, withdrawalNumber, merchantID, tokenID, id.New(), strings.Repeat("a", 64)); err != nil {
			return err
		}

		scopedCtx := merchant.WithPortalScope(txCtx, merchant.PortalScope{
			MerchantIDs: []id.ID{merchantID},
			Roles:       map[id.ID]merchant.PortalRole{merchantID: merchant.PortalRoleOwner},
		})
		repo := NewDashboardRepo()
		invoices, total, err := repo.ListInvoices(scopedCtx, &merchantID, InvoiceFilter{}, 10, 0)
		if err != nil {
			return err
		}
		if total != 1 || len(invoices) != 1 {
			return fmt.Errorf("invoice fixture mismatch: total=%d items=%d", total, len(invoices))
		}
		if invoices[0].ExplorerURL != explorerURL {
			return fmt.Errorf("invoice explorer URL mismatch: got=%q want=%q", invoices[0].ExplorerURL, explorerURL)
		}
		invoiceDetail, err := repo.GetInvoiceDetail(scopedCtx, invoiceID, &merchantID)
		if err != nil {
			return err
		}
		if invoiceDetail.ExplorerURL != explorerURL {
			return fmt.Errorf("invoice detail explorer URL mismatch: got=%q want=%q", invoiceDetail.ExplorerURL, explorerURL)
		}
		if invoiceDetail.CheckoutRef != publicRef {
			return fmt.Errorf("invoice detail checkout ref mismatch: got=%q want=%q", invoiceDetail.CheckoutRef, publicRef)
		}

		withdrawals, total, err := repo.ListWithdrawals(scopedCtx, &merchantID, WithdrawalFilter{}, 10, 0)
		if err != nil {
			return err
		}
		if total != 1 || len(withdrawals) != 1 {
			return fmt.Errorf("withdrawal fixture mismatch: total=%d items=%d", total, len(withdrawals))
		}
		if withdrawals[0].ExplorerURL != explorerURL {
			return fmt.Errorf("withdrawal explorer URL mismatch: got=%q want=%q", withdrawals[0].ExplorerURL, explorerURL)
		}

		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

func TestGetCurrenciesIncludesActiveTokensWithoutInvoiceHistory(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	var activeTokenCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cat_tokens t
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE t.is_active = TRUE
			AND t.deletion_mark = FALSE
			AND t._deleted_at IS NULL
			AND n.is_active = TRUE
			AND n.deletion_mark = FALSE
			AND n._deleted_at IS NULL`).Scan(&activeTokenCount)
	require.NoError(t, err)
	if activeTokenCount == 0 {
		t.Skip("test database has no active crypto tokens")
	}

	merchantID := id.New()
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	ctx = merchant.WithPortalScope(ctx, merchant.PortalScope{
		MerchantIDs: []id.ID{merchantID},
		Roles:       map[id.ID]merchant.PortalRole{merchantID: merchant.PortalRoleOwner},
	})

	items, err := NewDashboardRepo().GetCurrencies(ctx, &merchantID)
	require.NoError(t, err)
	require.Len(t, items, activeTokenCount)
	for _, item := range items {
		require.Equal(t, 0, item.Count)
		require.Equal(t, "0", item.TotalMinor)
		require.Equal(t, "0.00", item.InvoiceSharePct)
	}
}
