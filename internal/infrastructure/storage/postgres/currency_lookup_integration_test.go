//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

func TestCurrencyCodesKeepMarkedCurrencies(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)

	rollback := errors.New("rollback currency code fixture")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		active, marked, missing := id.New(), id.New(), id.New()
		_, err := q.Exec(ctx, `INSERT INTO cat_currencies(id,code,name,iso_code,symbol,decimal_places,minor_multiplier,is_base,version,deletion_mark,attributes)
			VALUES($1,'ZZA','Active test','ZZA','A',2,100,FALSE,1,FALSE,'{}'),($2,'ZZM','Marked test','ZZM','M',2,100,FALSE,1,TRUE,'{}')`, active, marked)
		require.NoError(t, err)

		codes, err := CurrencyCodes(ctx, []id.ID{active, marked, missing, active})
		require.NoError(t, err)
		require.Equal(t, map[id.ID]string{active: "ZZA", marked: "ZZM"}, codes, "a marked currency keeps its code, a missing one has none")
		empty, err := CurrencyCodes(ctx, nil)
		require.NoError(t, err)
		require.Empty(t, empty)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
