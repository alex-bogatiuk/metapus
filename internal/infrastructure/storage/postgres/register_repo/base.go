// Package register_repo provides PostgreSQL implementations for register repositories.
// BaseAccumulationRepo[T] provides generic CRUD operations shared across all accumulation registers.
package register_repo

import (
	"context"
	"fmt"

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
