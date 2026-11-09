//go:build integration

package rate_feed

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	er "metapus/internal/domain/registers/exchange_rate"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateWorkerReloadsConfigurationAndPreservesPartialResults(t *testing.T) {
	dsn := testsupport.ExchangeRateDSN(t)
	require.NotEmpty(t, dsn)
	output, err := migration.RunAll(dsn)
	require.NoError(t, err, output)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	ctx = appctx.WithUser(ctx, &appctx.UserContext{UserID: id.New().String(), Permissions: []string{"catalog:rate_source:update"}})
	source := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_rate_sources(id,code,name,source_type,api_key) VALUES($1,$2,'Worker fixture','coingecko','local-test')`, source, "WORKER-"+id.Compact(source))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_currencies(code,name,iso_code,symbol) VALUES('USD','US Dollar','USD','$'),('USDT','Tether','USDT','USDT'),('BTC','Bitcoin','BTC','BTC') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE sys_settings SET exchange_rates=jsonb_build_object('sourceId',$1::text),version=version+1`, source)
	require.NoError(t, err)
	repo := register_repo.NewExchangeRateRepo()
	cfg, err := repo.LoadConfiguration(ctx)
	require.NoError(t, err)
	var calls atomic.Int32
	var rotatedKeyCalls atomic.Int32
	var mode atomic.Int32
	started, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("x-cg-demo-api-key") == "rotated-test" {
			rotatedKeyCalls.Add(1)
		}
		switch mode.Load() {
		case 1:
			w.Header().Set("Retry-After", "1800")
			w.WriteHeader(429)
			return
		case 2:
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = fmt.Fprintf(w, `{"tether":{"usd":0.999712345678912345,"last_updated_at":%d},"bitcoin":{"usd":"bad","last_updated_at":%d}}`, time.Now().Add(-time.Minute).Unix(), time.Now().Unix())
	}))
	defer server.Close()
	fetcher := NewCoinGeckoFetcher()
	fetcher.baseURL = server.URL
	worker := &Worker{repo: repo, fetcher: fetcher}
	_, err = worker.doFetch(ctx)
	require.NoError(t, err)
	require.Zero(t, calls.Load(), "no mappings at startup")
	add := func(code, external string) er.Mapping {
		var currency id.ID
		require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM cat_currencies WHERE iso_code=$1`, code).Scan(&currency))
		m, e := repo.PutMapping(ctx, er.Mapping{Currency: er.CurrencyRef{ID: currency}, BaseCurrency: cfg.BaseCurrency, SourceID: source, ExternalID: external, IsActive: true}, 0)
		require.NoError(t, e)
		return m
	}
	m := add("USDT", "tether")
	add("BTC", "bitcoin")
	due := func() {
		_, e := pool.Exec(ctx, `UPDATE sys_rate_feed_state SET next_run_at=clock_timestamp()-interval '1 second' WHERE rate_source_id=$1`, source)
		require.NoError(t, e)
	}
	due()
	_, err = worker.doFetch(ctx)
	require.Error(t, err, "partial response is recorded as a worker diagnostic")
	state, err := repo.FeedState(ctx, source, cfg.BaseCurrency.ID)
	require.NoError(t, err)
	require.Equal(t, "partial", state.Status)
	require.Equal(t, 2, state.Requested)
	require.Equal(t, 1, state.Received)
	require.Equal(t, 1, state.Inserted)
	require.Equal(t, 1, state.Rejected)
	history, err := repo.History(ctx, er.HistoryFilter{CurrencyID: m.Currency.ID, BaseCurrencyID: cfg.BaseCurrency.ID, SourceID: &source, From: time.Now().Add(-time.Hour), To: time.Now(), Limit: 100})
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "0.999712345678912345", history[0].Rate.String())
	_, err = pool.Exec(ctx, `UPDATE cat_rate_sources SET api_key='rotated-test',version=version+1 WHERE id=$1`, source)
	require.NoError(t, err)
	due()
	_, err = worker.doFetch(ctx)
	require.Error(t, err, "partial provider response remains diagnostic after credential rotation")
	require.Positive(t, rotatedKeyCalls.Load(), "worker must reload the catalog credential without restart")
	due()
	mode.Store(1)
	_, err = worker.doFetch(ctx)
	require.Error(t, err)
	state, err = repo.FeedState(ctx, source, cfg.BaseCurrency.ID)
	require.NoError(t, err)
	require.True(t, state.NextRunAt.After(time.Now().Add(29*time.Minute)))
	selected, err := er.NewService(repo).ResolveBatch(ctx, cfg, []id.ID{m.Currency.ID}, time.Now())
	require.NoError(t, err)
	require.Equal(t, er.StatusFresh, selected[m.Currency.ID].Status, "HTTP error must not invalidate fresh price")
	due()
	mode.Store(2)
	completed := make(chan error, 1)
	go func() { _, e := worker.doFetch(ctx); completed <- e }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not start")
	}
	second := &Worker{repo: repo, fetcher: fetcher}
	work, err := second.doFetch(ctx)
	require.NoError(t, err)
	require.Zero(t, work)
	m.ExternalID = "usd-coin"
	_, err = repo.PutMapping(ctx, m, m.Version)
	require.NoError(t, err)
	close(release)
	require.Error(t, <-completed)
	state, err = repo.FeedState(ctx, source, cfg.BaseCurrency.ID)
	require.NoError(t, err)
	require.Zero(t, state.Inserted)
	require.Equal(t, 2, state.Rejected)
	due()
	_, err = pool.Exec(ctx, `UPDATE cat_rate_sources SET api_key=NULL,version=version+1 WHERE id=$1`, source)
	require.NoError(t, err)
	_, err = worker.doFetch(ctx)
	require.Error(t, err)
	state, err = repo.FeedState(ctx, source, cfg.BaseCurrency.ID)
	require.NoError(t, err)
	require.Equal(t, "api_key_missing", state.Problems[0].Code)
}
