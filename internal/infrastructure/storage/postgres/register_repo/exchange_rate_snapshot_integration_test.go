//go:build integration

package register_repo_test

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/application/exchangerateapp"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	er "metapus/internal/domain/registers/exchange_rate"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"net/url"
	"testing"
	"time"
)

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
	ctx, pool, repo, cfg, _ := rateFixture(t)
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
	service := exchangerateapp.NewValuationService(wrapper, er.NewService(repo), register_repo.RateSnapshot)
	first, err := service.Calculate(ctx, []id.ID{merchant})
	require.NoError(t, err)
	require.Len(t, first.ByToken, 1)
	require.Equal(t, "15", first.ByToken[0].RawAmount)
	require.Equal(t, "5", first.ByToken[0].PendingRaw)
	require.Equal(t, "10", first.ByToken[0].AvailableRaw)
	latest, err := exchangerateapp.NewValuationService(balances, er.NewService(repo), register_repo.RateSnapshot).Calculate(ctx, []id.ID{merchant})
	require.NoError(t, err)
	require.Equal(t, "15", latest.ByToken[0].RawAmount)
	require.Equal(t, "0", latest.ByToken[0].PendingRaw)
	require.Equal(t, "15", latest.ByToken[0].AvailableRaw)
	require.Equal(t, cfg.BaseCurrency.ID, latest.BaseCurrency.ID)
}

func TestExchangeRateTenantDatabaseIsolation(t *testing.T) {
	ctx, pool, repo, cfg, m := rateFixture(t)
	token := id.New()
	ok, err := repo.Claim(ctx, cfg, token)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = repo.Finish(ctx, cfg, token, er.FetchResult{Requested: 1, Observations: []er.Observation{observation(m, time.Now().Add(-time.Minute), "1")}})
	require.NoError(t, err)
	history, err := repo.History(ctx, er.HistoryFilter{CurrencyID: m.Currency.ID, BaseCurrencyID: cfg.BaseCurrency.ID, SourceID: &cfg.Source.ID, From: time.Now().Add(-time.Hour), To: time.Now(), Limit: 50})
	require.NoError(t, err)
	require.Len(t, history, 1)
	// A separate newly created database models the actual tenant boundary.
	name := "exchange_isolation_" + id.Compact(id.New())
	_, err = pool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	target, err := url.Parse(pool.Config().ConnString())
	require.NoError(t, err)
	target.Path = "/" + name
	output, err := migration.RunAll(target.String())
	require.NoError(t, err, output)
	otherPool, err := pgxpool.New(context.Background(), target.String())
	require.NoError(t, err)
	defer otherPool.Close()
	otherCtx := tenant.WithTxManager(context.Background(), postgres.NewTxManagerFromRawPool(otherPool))
	_, err = repo.GetQuote(otherCtx, history[0].ID)
	require.Error(t, err)
	var count int
	require.NoError(t, otherPool.QueryRow(otherCtx, `SELECT count(*) FROM reg_exchange_rates`).Scan(&count))
	require.Zero(t, count)
	// Re-reading the original database still returns its immutable observation.
	q, err := repo.GetQuote(ctx, history[0].ID)
	require.NoError(t, err)
	require.Equal(t, "1", q.Rate.String())
}
