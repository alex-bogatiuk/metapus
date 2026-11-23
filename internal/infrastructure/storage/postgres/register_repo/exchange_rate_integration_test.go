//go:build integration

package register_repo_test

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/application/exchangerateapp"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	er "github.com/alex-bogatiuk/metapus/internal/domain/registers/exchange_rate"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/register_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func rateFixture(t *testing.T) (context.Context, *pgxpool.Pool, *register_repo.ExchangeRateRepo, er.Configuration, er.Mapping) {
	t.Helper()
	dsn := testsupport.ExchangeRateDSN(t)
	require.NotEmpty(t, dsn, "use an isolated fresh database")
	output, err := migration.RunAll(dsn)
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
	repo := register_repo.NewExchangeRateRepo()
	cfg, err := repo.LoadConfiguration(ctx)
	require.NoError(t, err)
	var currency id.ID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM cat_currencies WHERE iso_code='USDT'`).Scan(&currency))
	m, err := repo.PutMapping(ctx, er.Mapping{SourceID: source, Currency: er.CurrencyRef{ID: currency}, BaseCurrency: cfg.BaseCurrency, ExternalID: "tether", IsActive: true}, 0)
	require.NoError(t, err)
	return ctx, pool, repo, cfg, m
}
func due(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg er.Configuration) {
	t.Helper()
	_, err := pool.Exec(ctx, `UPDATE sys_rate_feed_state SET next_run_at=clock_timestamp()-interval '1 second' WHERE rate_source_id=$1`, cfg.Source.ID)
	require.NoError(t, err)
}
func observation(m er.Mapping, stamp time.Time, price string) er.Observation {
	return er.Observation{Mapping: m, Rate: decimal.RequireFromString(price), ProviderUpdatedAt: stamp, ReceivedAt: time.Now().UTC()}
}
func TestExchangeRateHistoryVersionsAndFencing(t *testing.T) {
	ctx, pool, repo, cfg, m := rateFixture(t)
	stamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	ingest := func(price string, at time.Time) er.FeedState {
		due(t, ctx, pool, cfg)
		token := id.New()
		ok, err := repo.Claim(ctx, cfg, token)
		require.NoError(t, err)
		require.True(t, ok)
		input := er.FetchResult{Requested: 1, Observations: []er.Observation{observation(m, at, price)}}
		state, err := repo.Finish(ctx, cfg, token, input)
		require.NoError(t, err)
		replay, err := repo.Finish(ctx, cfg, token, input)
		require.NoError(t, err)
		require.Equal(t, state.Inserted, replay.Inserted)
		return state
	}
	require.Equal(t, 1, ingest("1", stamp).Inserted)
	require.Equal(t, 1, ingest("1", stamp).Repeated)
	require.Equal(t, 1, ingest("2", stamp).Inserted)
	require.Equal(t, 1, ingest("1", stamp).Inserted)
	require.Equal(t, 1, ingest("1", stamp.Add(time.Second)).Inserted)
	history, err := repo.History(ctx, er.HistoryFilter{CurrencyID: m.Currency.ID, BaseCurrencyID: cfg.BaseCurrency.ID, SourceID: &cfg.Source.ID, From: stamp.Add(-time.Second), To: time.Now(), Limit: 100})
	require.NoError(t, err)
	require.Len(t, history, 4)
	require.Equal(t, []int{1, 3, 2, 1}, []int{history[0].Version, history[1].Version, history[2].Version, history[3].Version})
	_, err = pool.Exec(ctx, `UPDATE reg_exchange_rates SET rate=3 WHERE id=$1`, history[0].ID)
	require.Error(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM reg_exchange_rates WHERE id=$1`, history[0].ID)
	require.Error(t, err)
	_, err = pool.Exec(ctx, `UPDATE cat_rate_sources SET source_type='manual' WHERE id=$1`, cfg.Source.ID)
	require.Error(t, err)
	selected, err := er.NewService(repo).ResolveBatch(ctx, cfg, []id.ID{m.Currency.ID, cfg.BaseCurrency.ID}, time.Now())
	require.NoError(t, err)
	require.Equal(t, er.StatusFresh, selected[m.Currency.ID].Status)
	require.Equal(t, er.StatusIdentity, selected[cfg.BaseCurrency.ID].Status)
	app := exchangerateapp.NewRegisterService(repo, register_repo.RateSnapshot)
	page, err := app.Current(ctx, exchangerateapp.CurrentRequest{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)
	_, err = app.Current(ctx, exchangerateapp.CurrentRequest{Cursor: *page.NextCursor, Status: er.StatusFresh})
	require.Error(t, err)
	request := exchangerateapp.HistoryRequest{CurrencyID: m.Currency.ID, SourceID: &cfg.Source.ID, Limit: 2}
	hp, err := app.History(ctx, request)
	require.NoError(t, err)
	require.Len(t, hp.Items, 2)
	require.NotNil(t, hp.NextCursor)
	request.Cursor = *hp.NextCursor
	next, err := app.History(ctx, request)
	require.NoError(t, err)
	require.Len(t, next.Items, 2)
	require.NotEqual(t, hp.Items[1].ID, next.Items[0].ID)
	t.Run("parallel claim and expired owner", func(t *testing.T) {
		due(t, ctx, pool, cfg)
		var accepted atomic.Int32
		var wg sync.WaitGroup
		var owner id.ID
		var mu sync.Mutex
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				token := id.New()
				ok, e := repo.Claim(ctx, cfg, token)
				if e != nil {
					t.Error(e)
				}
				if ok {
					accepted.Add(1)
					mu.Lock()
					owner = token
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		require.Equal(t, int32(1), accepted.Load())
		renewed, err := repo.Renew(ctx, cfg, owner)
		require.NoError(t, err)
		require.True(t, renewed)
		_, err = pool.Exec(ctx, `UPDATE sys_rate_feed_state SET lease_until=clock_timestamp()-interval '1 second' WHERE rate_source_id=$1`, cfg.Source.ID)
		require.NoError(t, err)
		renewed, err = repo.Renew(ctx, cfg, owner)
		require.NoError(t, err)
		require.False(t, renewed)
		replacement := id.New()
		ok, err := repo.Claim(ctx, cfg, replacement)
		require.NoError(t, err)
		require.True(t, ok)
		_, err = repo.Finish(ctx, cfg, owner, er.FetchResult{Requested: 1, Observations: []er.Observation{observation(m, stamp, "5")}})
		require.Error(t, err)
		state, err := repo.Finish(ctx, cfg, replacement, er.FetchResult{Requested: 1, Observations: []er.Observation{observation(m, stamp, "5")}})
		require.NoError(t, err)
		require.Equal(t, 1, state.Inserted)
	})
	t.Run("mapping changed during fetch", func(t *testing.T) {
		due(t, ctx, pool, cfg)
		token := id.New()
		ok, err := repo.Claim(ctx, cfg, token)
		require.NoError(t, err)
		require.True(t, ok)
		updated := m
		updated.ExternalID = "usd-coin"
		updated, err = repo.PutMapping(ctx, updated, m.Version)
		require.NoError(t, err)
		_, err = repo.PutMapping(ctx, m, m.Version)
		require.Error(t, err)
		state, err := repo.Finish(ctx, cfg, token, er.FetchResult{Requested: 1, Observations: []er.Observation{observation(m, stamp, "6")}})
		require.NoError(t, err)
		require.Equal(t, 1, state.Rejected)
		require.Zero(t, state.Inserted)
		rates, err := er.NewService(repo).ResolveBatch(ctx, cfg, []id.ID{m.Currency.ID}, time.Now())
		require.NoError(t, err)
		require.Equal(t, "mapping_changed", rates[m.Currency.ID].Reason)
		require.NotNil(t, rates[m.Currency.ID].LastKnownQuote)
		m = updated
	})
	t.Run("failed insert rolls back all quotes", func(t *testing.T) {
		due(t, ctx, pool, cfg)
		token := id.New()
		ok, err := repo.Claim(ctx, cfg, token)
		require.NoError(t, err)
		require.True(t, ok)
		_, err = repo.Finish(ctx, cfg, token, er.FetchResult{Requested: 2, Observations: []er.Observation{observation(m, stamp, "7"), observation(m, stamp.Add(2*time.Second), "8")}})
		require.Error(t, err, "duplicate pair in same attempt must reject entire packet")
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_exchange_rates WHERE feed_attempt_id=$1`, token).Scan(&count))
		require.Zero(t, count)
		require.NoError(t, repo.Fail(ctx, cfg, token, "test storage failure", 2))
		state, err := repo.FeedState(ctx, cfg.Source.ID, cfg.BaseCurrency.ID)
		require.NoError(t, err)
		require.Equal(t, "failed", state.Status)
	})
}

func TestExchangeRateLeaseMustRemainValidThroughCommit(t *testing.T) {
	ctx, pool, repo, cfg, m := rateFixture(t)
	token := id.New()
	ok, err := repo.Claim(ctx, cfg, token)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = pool.Exec(ctx, `CREATE FUNCTION test_expire_feed_lease() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN UPDATE sys_rate_feed_state SET lease_until=clock_timestamp()-interval '1 second' WHERE lease_token=NEW.feed_attempt_id; RETURN NEW; END; $$;
 CREATE TRIGGER test_expire_feed_lease AFTER INSERT ON reg_exchange_rates FOR EACH ROW EXECUTE FUNCTION test_expire_feed_lease();`)
	require.NoError(t, err)
	_, err = repo.Finish(ctx, cfg, token, er.FetchResult{Requested: 1, Observations: []er.Observation{observation(m, time.Now().Add(-time.Minute), "1")}})
	require.Error(t, err)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_exchange_rates WHERE feed_attempt_id=$1`, token).Scan(&count))
	require.Zero(t, count)
}
