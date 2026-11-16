//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestOverviewReceiptsAndPendingUseCanonicalPayments(t *testing.T) {
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
	rollback := errors.New("rollback overview fixture")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var tokenA, tokenB, networkA, networkB id.ID
		require.NoError(t, q.QueryRow(txCtx, `SELECT id, network_id FROM cat_tokens WHERE _deleted_at IS NULL AND deletion_mark=FALSE ORDER BY id LIMIT 1`).Scan(&tokenA, &networkA))
		require.NoError(t, q.QueryRow(txCtx, `SELECT id, network_id FROM cat_tokens WHERE _deleted_at IS NULL AND deletion_mark=FALSE AND id<>$1 ORDER BY id LIMIT 1`, tokenA).Scan(&tokenB, &networkB))
		merchantID, otherMerchant, invoiceID := id.New(), id.New(), id.New()
		underpaidInvoice := id.New()
		require.NoError(t, func() error {
			_, e := q.Exec(txCtx, `INSERT INTO doc_crypto_invoices
				(id,public_ref,number,date,merchant_id,token_id,expected_amount,status,expires_at,created_at)
				VALUES ($1,$2,$3,NOW()-INTERVAL '120 days',$4,$5,10000000,'completed',NOW()+INTERVAL '1 hour',NOW()-INTERVAL '120 days')`,
				invoiceID, "ci_"+id.Compact(invoiceID), "OV-"+id.Compact(invoiceID), merchantID, tokenA)
			return e
		}())
		_, err := q.Exec(txCtx, `INSERT INTO doc_crypto_invoices
			(id,public_ref,number,date,merchant_id,token_id,expected_amount,status,expires_at)
			VALUES ($1,$2,$3,NOW()-INTERVAL '120 days',$4,$5,10000000,'underpaid',NOW()-INTERVAL '1 day')`,
			underpaidInvoice, "ci_"+id.Compact(underpaidInvoice), "OV-"+id.Compact(underpaidInvoice), merchantID, tokenB)
		require.NoError(t, err)
		var today time.Time
		require.NoError(t, q.QueryRow(txCtx, `SELECT (NOW() AT TIME ZONE 'UTC')::date`).Scan(&today))
		for _, p := range []struct {
			merchant, token, network     id.ID
			amount, status, canonicality string
			posted                       bool
			daysAgo                      int
		}{
			{merchantID, tokenA, networkA, "1000000", "settled", "canonical", true, 0},
			{merchantID, tokenA, networkA, "9000000", "confirmed", "canonical", true, 0},
			{merchantID, tokenB, networkB, "123456789012345678901234567890", "settled", "canonical", true, 0},
			{merchantID, tokenA, networkA, "7", "settled", "canonical", true, 6},
			{merchantID, tokenA, networkA, "30", "settled", "canonical", true, 29},
			{merchantID, tokenA, networkA, "90", "settled", "canonical", true, 89},
			{merchantID, tokenA, networkA, "999", "settled", "canonical", true, 90},
			{merchantID, tokenA, networkA, "999", "confirmed", "orphaned", true, 0},
			{merchantID, tokenA, networkA, "999", "failed", "canonical", false, 0},
			{merchantID, tokenA, networkA, "999", "settled", "canonical", true, -1},
			{merchantID, tokenA, networkA, "2", "detected", "canonical", false, 0},
			{merchantID, tokenA, networkA, "3", "confirming", "canonical", false, 0},
			{merchantID, tokenB, networkB, "4", "confirmed", "canonical", false, 0},
			{otherMerchant, tokenA, networkA, "999", "settled", "canonical", true, 0},
			{otherMerchant, tokenA, networkA, "999", "detected", "canonical", false, 0},
		} {
			paymentInvoice := invoiceID
			if p.token == tokenB {
				paymentInvoice = underpaidInvoice
			}
			_, e := q.Exec(txCtx, `INSERT INTO doc_crypto_payments
				(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,canonicality,posted,confirmed_at,date)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW()-INTERVAL '120 days')`,
				id.New(), paymentInvoice, p.merchant, p.network, p.token, id.New(), id.New().String(), p.amount,
				p.status, p.canonicality, p.posted, today.AddDate(0, 0, -p.daysAgo).Add(12*time.Hour))
			require.NoError(t, e)
		}
		scoped := merchant.WithPortalScope(txCtx, merchant.PortalScope{MerchantIDs: []id.ID{merchantID}})
		repo := NewDashboardRepo()
		for _, period := range []struct {
			name string
			want []string
		}{
			{"7d", []string{"7", "10000000"}},
			{"30d", []string{"30", "7", "10000000"}},
			{"90d", []string{"90", "30", "7", "10000000"}},
		} {
			points, e := repo.GetChart(scoped, period.name, &merchantID)
			require.NoError(t, e)
			var got []string
			for _, point := range points {
				require.NotEmpty(t, point.Symbol)
				require.NotEmpty(t, point.Network)
				if point.TokenID == tokenA.String() {
					got = append(got, point.Deposits)
				}
				if point.TokenID == tokenB.String() {
					require.Equal(t, "123456789012345678901234567890", point.Deposits)
				}
			}
			require.Equal(t, period.want, got)
			require.Len(t, points, len(period.want)+1)
		}
		unauthorized, e := repo.GetChart(scoped, "30d", &otherMerchant)
		require.NoError(t, e)
		require.Empty(t, unauthorized)
		pending, e := repo.GetPendingAmounts(scoped, []id.ID{merchantID})
		require.NoError(t, e)
		require.Equal(t, "5", pending[tokenA.String()].String())
		require.Equal(t, "4", pending[tokenB.String()].String())
		balances, e := repo.GetMerchantBalances(scoped, []id.ID{merchantID})
		require.NoError(t, e)
		require.Len(t, balances, 2)
		for _, balance := range balances {
			require.NotEmpty(t, balance.Network)
			require.False(t, id.IsNil(balance.NetworkID))
			require.Equal(t, "0", balance.RawAmount.String())
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
