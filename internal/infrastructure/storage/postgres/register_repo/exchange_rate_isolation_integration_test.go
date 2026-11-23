//go:build integration

package register_repo_test

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	er "github.com/alex-bogatiuk/metapus/internal/domain/registers/exchange_rate"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"net/url"
	"testing"
	"time"
)

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
