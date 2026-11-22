//go:build integration

package portal_repo_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/cryptoschema"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/portal_repo"
	"metapus/pkg/sdk/appctx"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/migration"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/registers"
	"metapus/pkg/sdk/registers/exchange_rate"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"
)

// valuationFixture is a fresh database with a configured rate source and a
// USDT mapping, as the platform's exchange-rate tests use.
func valuationFixture(t *testing.T) (context.Context, *pgxpool.Pool, *registers.ExchangeRateRepo, exchange_rate.Configuration) {
	t.Helper()
	dsn := sdktest.NewExchangeRateDatabase(t)
	output, err := migration.RunAll(dsn, cryptoschema.Migration())
	require.NoError(t, err, output)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	ctx = appctx.WithUser(ctx, &appctx.UserContext{UserID: id.New().String(), Permissions: []string{"catalog:rate_source:update", "catalog:rate_source:read"}})
	source := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_rate_sources(id,code,name,source_type) VALUES($1,$2,'Integration CoinGecko','coingecko')`, source, "RATE-"+id.Compact(source))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_currencies(code,name,iso_code,symbol) VALUES('USD','US Dollar','USD','$') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_currencies(code,name,iso_code,symbol) VALUES('USDT','Tether','USDT','USDT') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE sys_settings SET exchange_rates=jsonb_build_object('sourceId',$1::text),version=version+1`, source)
	require.NoError(t, err)
	repo := registers.NewExchangeRateRepo()
	cfg, err := repo.LoadConfiguration(ctx)
	require.NoError(t, err)
	var currency id.ID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM cat_currencies WHERE iso_code='USDT'`).Scan(&currency))
	_, err = repo.PutMapping(ctx, exchange_rate.Mapping{SourceID: source, Currency: exchange_rate.CurrencyRef{ID: currency}, BaseCurrency: cfg.BaseCurrency, ExternalID: "tether", IsActive: true}, 0)
	require.NoError(t, err)
	return ctx, pool, repo, cfg
}

type snapshotBalances struct {
	crypto.BalanceQueryRepo
	afterRead func()
}

func (r *snapshotBalances) GetMerchantBalances(ctx context.Context, ids []id.ID) ([]crypto.BalanceRow, error) {
	rows, err := r.BalanceQueryRepo.GetMerchantBalances(ctx, ids)
	if err == nil && r.afterRead != nil {
		r.afterRead()
	}
	return rows, err
}
func TestExchangeRateValuationUsesOneRepeatableSnapshot(t *testing.T) {
	ctx, pool, repo, cfg := valuationFixture(t)
	var token, network id.ID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id,network_id FROM cat_tokens ORDER BY id LIMIT 1`).Scan(&token, &network))
	merchant, other, invoice, payment := id.New(), id.New(), id.New(), id.New()
	_, err := pool.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_balances(merchant_id,token_id,amount) VALUES($1,$2,10),($3,$2,1000)`, merchant, token, other)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,expected_amount,received_amount,status,expires_at) VALUES($1,$2,$3,now(),$4,$5,5,5,'paid',now()+interval '1 hour')`, invoice, "ci_"+id.Compact(invoice), "SNAP-"+id.Compact(invoice), merchant, token)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,canonicality,posted)
		VALUES($1,$2,$3,$4,$5,$6,$7,5,'confirmed','canonical',FALSE)`, payment, invoice, merchant, network, token, id.New(), "snapshot-"+id.Compact(payment))
	require.NoError(t, err)
	balances := portal_repo.NewDashboardRepo()
	wrapper := &snapshotBalances{BalanceQueryRepo: balances, afterRead: func() {
		// Credit the pending payment between the balance and pending reads.
		// Both reads must still observe the state before this commit.
		tx, e := pool.Begin(ctx)
		require.NoError(t, e)
		defer func() { _ = tx.Rollback(ctx) }()
		_, e = tx.Exec(ctx, `UPDATE reg_crypto_merchant_balance_balances SET amount=15 WHERE merchant_id=$1 AND token_id=$2`, merchant, token)
		require.NoError(t, e)
		_, e = tx.Exec(ctx, `UPDATE doc_crypto_invoices SET status='confirmed' WHERE id=$1`, invoice)
		require.NoError(t, e)
		// Only the payment's pending lifecycle matters here; its attribution
		// to a receipt is outside this read-consistency check.
		_, e = tx.Exec(ctx, `UPDATE doc_crypto_payments SET status='settled' WHERE id=$1`, payment)
		require.NoError(t, e)
		require.NoError(t, tx.Commit(ctx))
	}}
	service := cryptoapp.NewValuationService(wrapper, exchange_rate.NewService(repo), registers.RateSnapshot)
	first, err := service.Calculate(ctx, []id.ID{merchant})
	require.NoError(t, err)
	require.Len(t, first.ByToken, 1)
	require.Equal(t, "15", first.ByToken[0].RawAmount)
	require.Equal(t, "5", first.ByToken[0].PendingRaw)
	require.Equal(t, "10", first.ByToken[0].AvailableRaw)
	latest, err := cryptoapp.NewValuationService(balances, exchange_rate.NewService(repo), registers.RateSnapshot).Calculate(ctx, []id.ID{merchant})
	require.NoError(t, err)
	require.Equal(t, "15", latest.ByToken[0].RawAmount)
	require.Equal(t, "0", latest.ByToken[0].PendingRaw)
	require.Equal(t, "15", latest.ByToken[0].AvailableRaw)
	require.Equal(t, cfg.BaseCurrency.ID, latest.BaseCurrency.ID)
}
