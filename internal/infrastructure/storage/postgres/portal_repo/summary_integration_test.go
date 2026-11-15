//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestSummaryKeepsDifferentAssetsSeparateAndSharesCountInvoices(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_CRYPTO_DSN is not set")
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	manager := postgres.NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(t.Context(), manager)
	rollback := errors.New("rollback summary fixture")
	err = manager.RunInTransaction(ctx, func(ctx context.Context) error {
		q := manager.GetQuerier(ctx)
		merchantID := id.New()
		_, err := q.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Summary test')`, merchantID)
		require.NoError(t, err)
		totals := map[string]string{}
		for i, amount := range []string{"1", "1000000000000000001"} {
			networkID, tokenID, invoiceID := id.New(), id.New(), id.New()
			_, err = q.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			 VALUES($1,$1::uuid::text,'Summary network','evm',$1::uuid::text,'ETH',18)`, networkID)
			require.NoError(t, err)
			_, err = q.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard)
			 VALUES($1,$1::uuid::text,'Summary token',$2,'ETH',$3,'native')`, tokenID, networkID, []int{6, 18}[i])
			require.NoError(t, err)
			_, err = q.Exec(ctx, `INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,expected_amount,received_amount,status,expires_at)
			 VALUES($1,$2,$1::uuid::text,NOW(),$3,$4,$5,$5,$6,NOW()+INTERVAL '1 hour')`, invoiceID, "ci_"+id.Compact(invoiceID), merchantID, tokenID, amount, []string{"confirmed", "completed"}[i])
			require.NoError(t, err)
			_, err = q.Exec(ctx, `INSERT INTO doc_crypto_payments(id,date,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,canonicality,posted,confirmed_at)
			 VALUES($1,NOW(),$2,$3,$4,$5,$6,$1::uuid::text,$7,'settled','canonical',TRUE,NOW()-INTERVAL '1 hour')`, id.New(), invoiceID, merchantID, networkID, tokenID, id.New(), amount)
			require.NoError(t, err)
			totals[tokenID.String()] = amount
		}
		ctx = merchant.WithPortalScope(ctx, merchant.PortalScope{MerchantIDs: []id.ID{merchantID}})
		repo := NewDashboardRepo()
		summary, err := repo.GetSummary(ctx, &merchantID)
		require.NoError(t, err)
		require.EqualValues(t, 2, summary.PaidInvoices)
		require.Len(t, summary.ReceiptTotals, 2)
		for _, total := range summary.ReceiptTotals {
			require.Equal(t, totals[total.TokenID], total.TotalMinor)
			require.Equal(t, totals[total.TokenID], total.Received24hMinor)
		}
		currencies, err := repo.GetCurrencies(ctx, &merchantID)
		require.NoError(t, err)
		matched := 0
		for _, currency := range currencies {
			if _, exists := totals[currency.TokenID]; exists {
				matched++
				require.Equal(t, "50.00", currency.InvoiceSharePct, "raw amount must not affect the invoice share")
			}
		}
		require.Equal(t, 2, matched)
		otherMerchant := id.New()
		unauthorized, err := repo.GetSummary(ctx, &otherMerchant)
		require.NoError(t, err)
		require.Empty(t, unauthorized.ReceiptTotals)
		require.Zero(t, unauthorized.TotalInvoices)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
