//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	coretx "metapus/internal/core/tx"
)

func TestTransactionRollsBackWhenCallbackCallsGoexit(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := NewTxManagerFromRawPool(pool)
	tableName := "tx_manager_goexit_" + id.Compact(id.New())
	quotedTable := pgx.Identifier{tableName}.Sanitize()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
			if _, createErr := txm.GetQuerier(txCtx).Exec(txCtx, "CREATE TABLE "+quotedTable+" (id integer)"); createErr != nil {
				panic(createErr)
			}
			runtime.Goexit()
			return nil
		})
	}()
	<-done

	var relationName *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass($1)::text", tableName).Scan(&relationName))
	require.Nil(t, relationName, "callback termination must roll back all transaction changes")
}

func TestTransactionAfterCommitTracksSavepointOutcome(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := NewTxManagerFromRawPool(pool)
	var called []string
	register := func(ctx context.Context, name string) error {
		return coretx.OnCommit(ctx, func(committed context.Context) {
			require.Nil(t, txm.GetTx(committed))
			called = append(called, name)
		})
	}
	require.Error(t, register(ctx, "outside"))
	for _, rollback := range []bool{true, false} {
		err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
			require.NoError(t, register(ctx, "outer"))
			require.NoError(t, txm.RunInSavepoint(ctx, func(ctx context.Context) error { return register(ctx, "released") }))
			require.Error(t, txm.RunInSavepoint(ctx, func(ctx context.Context) error {
				require.NoError(t, register(ctx, "rolled-back"))
				return errors.New("savepoint failure")
			}))
			require.NoError(t, txm.RunInTransaction(ctx, func(ctx context.Context) error { return register(ctx, "reused") }))
			require.Empty(t, called)
			if rollback {
				return errors.New("outer failure")
			}
			return nil
		})
		if rollback {
			require.Error(t, err)
			require.Empty(t, called)
		} else {
			require.NoError(t, err)
			require.Equal(t, []string{"outer", "released", "reused"}, called)
		}
	}
}
