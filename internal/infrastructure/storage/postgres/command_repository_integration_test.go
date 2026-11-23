//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/application/command"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

func TestConcurrentCommandExecutorRunsOneCallbackAndRollbackLeavesNoPendingOwner(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	executor := command.NewExecutor(txm, NewCommandRepository())

	suffix := id.Compact(id.New())
	merchantID := id.New()
	fingerprint, err := command.Fingerprint(map[string]any{"merchantId": merchantID.String(), "amount": "42000000"})
	require.NoError(t, err)
	metadata := command.Metadata{
		ScopeType: "system", ScopeID: "integration:" + suffix,
		Operation: "POST /integration/idempotency", IdempotencyKey: "same-first-second-" + suffix,
		RequestFingerprint: fingerprint,
	}

	var callbacks atomic.Int32
	type executionResult struct {
		execution command.Execution
		err       error
	}
	results := make(chan executionResult, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			execution, executeErr := executor.Execute(ctx, metadata, func(txCtx context.Context) (command.Result, error) {
				callbacks.Add(1)
				_, insertErr := txm.GetQuerier(txCtx).Exec(txCtx, `
					INSERT INTO cat_merchants(id,code,name)
					VALUES($1,$2,'Command idempotency integration merchant')
				`, merchantID, "IDEMPOTENCY-"+suffix)
				return command.Result{
					Outcome: "created", ResourceType: "Merchant", ResourceID: merchantID.String(), ResourceVersion: 1,
					Payload: json.RawMessage(fmt.Sprintf(`{"id":%q,"version":1}`, merchantID.String())),
				}, insertErr
			})
			results <- executionResult{execution: execution, err: executeErr}
		}()
	}
	wg.Wait()
	close(results)

	var firstExecutions, replays int
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, merchantID.String(), result.execution.Result.ResourceID)
		if result.execution.Replayed {
			replays++
		} else {
			firstExecutions++
		}
	}
	require.EqualValues(t, 1, callbacks.Load())
	require.Equal(t, 1, firstExecutions)
	require.Equal(t, 19, replays)

	var commandRows, merchantRows int
	var status string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*),MAX(status::text) FROM sys_idempotency
		WHERE scope_type=$1 AND scope_id=$2 AND operation=$3 AND idempotency_key=$4
	`, metadata.ScopeType, metadata.ScopeID, metadata.Operation, metadata.IdempotencyKey).Scan(&commandRows, &status))
	require.Equal(t, 1, commandRows)
	require.Equal(t, "success", status)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM cat_merchants WHERE id=$1`, merchantID).Scan(&merchantRows))
	require.Equal(t, 1, merchantRows)

	differentFingerprint, err := command.Fingerprint(map[string]any{"merchantId": merchantID.String(), "amount": "42000001"})
	require.NoError(t, err)
	mismatched := metadata
	mismatched.RequestFingerprint = differentFingerprint
	_, err = executor.Execute(ctx, mismatched, func(context.Context) (command.Result, error) {
		t.Fatal("mismatched fingerprint executed business callback")
		return command.Result{}, nil
	})
	require.ErrorIs(t, err, command.ErrKeyReused)

	rollbackMerchantID := id.New()
	rollbackFingerprint, err := command.Fingerprint(map[string]any{"merchantId": rollbackMerchantID.String()})
	require.NoError(t, err)
	rollbackMetadata := command.Metadata{
		ScopeType: "system", ScopeID: "integration:" + suffix,
		Operation: "POST /integration/idempotency/rollback", IdempotencyKey: "rollback-" + suffix,
		RequestFingerprint: rollbackFingerprint,
	}
	callbackFailure := errors.New("simulate process failure before command completion")
	_, err = executor.Execute(ctx, rollbackMetadata, func(txCtx context.Context) (command.Result, error) {
		_, insertErr := txm.GetQuerier(txCtx).Exec(txCtx, `
			INSERT INTO cat_merchants(id,code,name)
			VALUES($1,$2,'Rolled back idempotency merchant')
		`, rollbackMerchantID, "IDEMPOTENCY-ROLLBACK-"+suffix)
		if insertErr != nil {
			return command.Result{}, insertErr
		}
		return command.Result{}, callbackFailure
	})
	require.ErrorIs(t, err, callbackFailure)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM sys_idempotency WHERE scope_type=$1 AND scope_id=$2 AND operation=$3 AND idempotency_key=$4),
			(SELECT COUNT(*) FROM cat_merchants WHERE id=$5)
	`, rollbackMetadata.ScopeType, rollbackMetadata.ScopeID, rollbackMetadata.Operation,
		rollbackMetadata.IdempotencyKey, rollbackMerchantID).Scan(&commandRows, &merchantRows))
	require.Zero(t, commandRows)
	require.Zero(t, merchantRows)

	retry, err := executor.Execute(ctx, rollbackMetadata, func(txCtx context.Context) (command.Result, error) {
		_, insertErr := txm.GetQuerier(txCtx).Exec(txCtx, `
			INSERT INTO cat_merchants(id,code,name)
			VALUES($1,$2,'Retried idempotency merchant')
		`, rollbackMerchantID, "IDEMPOTENCY-ROLLBACK-"+suffix)
		return command.Result{Outcome: "created", ResourceType: "Merchant", ResourceID: rollbackMerchantID.String(), ResourceVersion: 1}, insertErr
	})
	require.NoError(t, err)
	require.False(t, retry.Replayed)
}
