// Package register_repo provides PostgreSQL implementations for register repositories.
// BaseAccumulationRepo[T] provides generic CRUD operations shared across all accumulation registers.
package register_repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// MovementRowMapper converts a typed movement into a flat row of values for COPY/INSERT.
type MovementRowMapper[T any] func(m T) []any

// BaseAccumulationRepo provides generic operations shared by all accumulation register repos:
// - CreateMovements (batch COPY)
// - DeleteMovementsByRecorder
// - GetMovementsByRecorder
//
// Register-specific queries (balances, turnovers) remain in concrete repos.
type BaseAccumulationRepo[T any] struct {
	movementsTable string
	columns        []string
	rowMapper      MovementRowMapper[T]
	builder        squirrel.StatementBuilderType
}

// appendReversalsByRecorder is used only by the five crypto financial
// repositories. The table and dimension identifiers are static package data,
// never request input. The ledger-reversal partial unique index makes retries safe.
func (r *BaseAccumulationRepo[T]) appendReversalsByRecorder(
	ctx context.Context, recorderID id.ID, beforeVersion int, dimensions []string,
) error {
	if beforeVersion <= 0 {
		return fmt.Errorf("positive reversal version is required")
	}
	columns := strings.Join(dimensions, ",")
	selected := make([]string, len(dimensions))
	for i, column := range dimensions {
		selected[i] = "m." + column
	}
	sql := fmt.Sprintf(`
		INSERT INTO %[1]s (
			line_id,recorder_id,recorder_type,recorder_version,period,
			record_type,%[2]s,amount,reversal_of_line_id,reversal_reason
		)
		SELECT gen_random_uuid_v7(),m.recorder_id,'CryptoLedgerReversal',$2,NOW(),
		       CASE WHEN m.record_type='receipt' THEN 'expense' ELSE 'receipt' END,
		       %[3]s,m.amount-COALESCE(reversed.amount,0),m.line_id,'posting_reversal'
		FROM %[1]s AS m
		LEFT JOIN LATERAL (SELECT SUM(r.amount) AS amount FROM %[1]s r
		    WHERE r.reversal_of_line_id=m.line_id) reversed ON TRUE
		WHERE m.recorder_id=$1 AND m.recorder_version<$2
		  AND m.reversal_of_line_id IS NULL
		  AND m.amount>COALESCE(reversed.amount,0)
		  AND NOT EXISTS (
		      SELECT 1 FROM %[1]s AS reversal
		      WHERE reversal.reversal_of_line_id=m.line_id
		        AND reversal.recorder_type='CryptoLedgerReversal'
		  )
		ON CONFLICT DO NOTHING
	`, r.movementsTable, columns, strings.Join(selected, ","))
	_, err := r.getTxManager(ctx).GetQuerier(ctx).Exec(ctx, sql, recorderID, beforeVersion)
	if err != nil {
		return fmt.Errorf("append %s reversals: %w", r.movementsTable, err)
	}
	return nil
}

// NewBaseAccumulationRepo creates a new base accumulation repo.
func NewBaseAccumulationRepo[T any](
	movementsTable string,
	columns []string,
	rowMapper MovementRowMapper[T],
) BaseAccumulationRepo[T] {
	return BaseAccumulationRepo[T]{
		movementsTable: movementsTable,
		columns:        columns,
		rowMapper:      rowMapper,
		builder:        squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// CreateMovements batch inserts movements using COPY (fast path) or INSERT (fallback).
func (r *BaseAccumulationRepo[T]) CreateMovements(ctx context.Context, movements []T) error {
	if len(movements) == 0 {
		return nil
	}

	movements = stampMovementMetadata(movements, time.Now().UTC())
	txm := r.getTxManager(ctx)

	// Fast path: COPY when inside a transaction.
	if tx := txm.GetTx(ctx); tx != nil {
		inserter := postgres.NewBatchInserter(txm)
		rows := make([][]any, 0, len(movements))
		for _, m := range movements {
			rows = append(rows, r.rowMapper(m))
		}
		if _, err := inserter.CopyFromSlice(ctx, r.movementsTable, r.columns, rows); err != nil {
			return fmt.Errorf("copy movements: %w", err)
		}
		return nil
	}

	// Fallback: non-transactional INSERT.
	q := r.builder.Insert(r.movementsTable).Columns(r.columns...)
	for _, m := range movements {
		q = q.Values(r.rowMapper(m)...)
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	querier := txm.GetQuerier(ctx)
	if _, err = querier.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert movements: %w", err)
	}

	return nil
}

// DeleteMovementsByRecorder removes movements for a document version.
func (r *BaseAccumulationRepo[T]) DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	q := r.builder.Delete(r.movementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		Where(squirrel.Lt{"recorder_version": beforeVersion})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if _, err = querier.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("delete movements: %w", err)
	}

	return nil
}

// getTxManager retrieves TxManager from context.
func (r *BaseAccumulationRepo[T]) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// Builder returns the squirrel builder for use in concrete repos.
func (r *BaseAccumulationRepo[T]) Builder() squirrel.StatementBuilderType {
	return r.builder
}

// MovementsTable returns the movements table name for use in concrete repos.
func (r *BaseAccumulationRepo[T]) MovementsTable() string {
	return r.movementsTable
}

// GetTxManager returns the TxManager from context (exported for concrete repos).
func (r *BaseAccumulationRepo[T]) GetTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// stampMovementMetadata copies plans before assigning persistence identity. The
// caller's deterministic plan remains reusable; every persisted row gets UUIDv7.
func stampMovementMetadata[T any](plans []T, at time.Time) []T {
	result := append([]T(nil), plans...)
	for i := range result {
		if movement, ok := any(&result[i]).(interface{ SetRecordingMetadata(id.ID, time.Time) }); ok {
			movement.SetRecordingMetadata(id.New(), at)
		}
	}
	return result
}
