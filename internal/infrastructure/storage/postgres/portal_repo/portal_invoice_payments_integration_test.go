//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestPortalInvoiceAggregatesMultiplePayments(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	rollback := errors.New("rollback invoice payments fixture")
	err = postgres.ReadSnapshot(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var tokenID, networkID id.ID
		err := q.QueryRow(txCtx, `SELECT id, network_id FROM cat_tokens WHERE _deleted_at IS NULL AND deletion_mark = FALSE ORDER BY created_at LIMIT 1`).Scan(&tokenID, &networkID)
		if errors.Is(err, pgx.ErrNoRows) {
			t.Skip("test database has no crypto token")
		}
		if err != nil {
			return err
		}
		merchantID, invoiceID, walletID := id.New(), id.New(), id.New()
		_, err = q.Exec(txCtx, `
			INSERT INTO doc_crypto_invoices
				(id, public_ref, number, date, merchant_id, token_id, expected_amount,
				received_amount, confirmed_amount, credited_amount, status, expires_at)
			VALUES ($1, $2, 'CI-IT-MULTI', NOW(), $3, $4, 10000000,
				10000000, 10000000, 10000000, 'completed', $5)`,
			invoiceID, "ci_"+id.Compact(invoiceID), merchantID, tokenID, time.Now().Add(time.Hour))
		if err != nil {
			return err
		}
		for _, payment := range []struct {
			amount, fee          int64
			status, canonicality string
		}{
			{1000000, 0, "settled", "canonical"},
			{9000000, 0, "settled", "canonical"},
			{2000000, 0, "reorged", "orphaned"},
		} {
			_, err = q.Exec(txCtx, `
				INSERT INTO doc_crypto_payments
					(id, invoice_id, merchant_id, network_id, token_id, wallet_id,
					tx_hash, amount, status, canonicality, posted, fee_fixed)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				id.New(), invoiceID, merchantID, networkID, tokenID, walletID,
				id.New().String(), payment.amount, payment.status, payment.canonicality,
				payment.canonicality == "canonical", payment.fee)
			if err != nil {
				return err
			}
		}
		scopedCtx := merchant.WithPortalScope(txCtx, merchant.PortalScope{
			MerchantIDs: []id.ID{merchantID},
			Roles:       map[id.ID]merchant.PortalRole{merchantID: merchant.PortalRoleOwner},
		})
		repo := NewDashboardRepo()
		items, total, err := repo.ListInvoices(scopedCtx, &merchantID, InvoiceFilter{}, 10, 0)
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, items, 1)
		require.Equal(t, 3, items[0].PaymentCount)
		require.Equal(t, "10000000", items[0].NetAmount)
		detail, err := repo.GetInvoiceDetail(scopedCtx, invoiceID, &merchantID)
		require.NoError(t, err)
		require.Equal(t, items[0].NetAmount, detail.NetAmount)
		require.Len(t, detail.Payments, 3)
		require.ElementsMatch(t, []string{"1000000", "9000000", "2000000"}, []string{
			detail.Payments[0].Amount, detail.Payments[1].Amount, detail.Payments[2].Amount,
		})
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
