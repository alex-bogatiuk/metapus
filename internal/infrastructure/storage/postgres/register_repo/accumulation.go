package register_repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// AccumulationStore keeps the movements and balances of declarative
// accumulation registers in the tables their schemas name. It holds no state:
// the transaction comes from the context.
type AccumulationStore struct {
	builder squirrel.StatementBuilderType
}

// NewAccumulationStore creates the store.
func NewAccumulationStore() *AccumulationStore {
	return &AccumulationStore{builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)}
}

// CreateMovements stamps each movement with its line id and recording time
// and inserts them: by COPY inside a transaction, by INSERT outside.
func (s *AccumulationStore) CreateMovements(ctx context.Context, schema *accumulation.Schema, movements []accumulation.Movement) error {
	if len(movements) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([][]any, len(movements))
	for i, m := range movements {
		m.Header().SetRecordingMetadata(id.New(), now)
		rows[i] = schema.Values(m)
	}
	columns := schema.MovementColumns()
	txm := postgres.MustGetTxManager(ctx)
	if txm.GetTx(ctx) != nil {
		if _, err := postgres.NewBatchInserter(txm).CopyFromSlice(ctx, schema.MovementsTable(), columns, rows); err != nil {
			return fmt.Errorf("copy movements: %w", err)
		}
		return nil
	}
	q := s.builder.Insert(schema.MovementsTable()).Columns(columns...)
	for _, row := range rows {
		q = q.Values(row...)
	}
	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}
	if _, err := txm.GetQuerier(ctx).Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert movements: %w", err)
	}
	return nil
}

// DeleteMovements removes the document's movements of versions before
// beforeVersion.
func (s *AccumulationStore) DeleteMovements(ctx context.Context, schema *accumulation.Schema, recorderID id.ID, beforeVersion int) error {
	sql, args, err := s.builder.Delete(schema.MovementsTable()).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		Where(squirrel.Lt{"recorder_version": beforeVersion}).ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}
	if _, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("delete movements: %w", err)
	}
	return nil
}

// SelectMovements reads the document's movements in the order they were
// recorded.
func (s *AccumulationStore) SelectMovements(ctx context.Context, schema *accumulation.Schema, recorderID id.ID, dst any) error {
	q := s.builder.Select(quoted(schema.MovementColumns())...).From(schema.MovementsTable()).
		Where(squirrel.Eq{"recorder_id": recorderID}).OrderBy("created_at", "line_id")
	return s.selectInto(ctx, q, dst)
}

// SelectBalances reads the balances table: the current balance of every
// combination the filter selects whose resources are not all zero.
func (s *AccumulationStore) SelectBalances(ctx context.Context, schema *accumulation.Schema, filter accumulation.Filter, dst any) error {
	q := s.builder.Select(quoted(schema.FieldColumns())...).From(schema.BalancesTable()).
		Where(squirrel.Eq(filter)).Where(nonZero(schema, "")).OrderBy(quoted(schema.DimensionColumns())...)
	return s.selectInto(ctx, q, dst)
}

// SelectBalancesAt sums the movements before at.
func (s *AccumulationStore) SelectBalancesAt(ctx context.Context, schema *accumulation.Schema, at time.Time, filter accumulation.Filter, dst any) error {
	q := s.sums(schema).Where(squirrel.Lt{"period": at}).Where(squirrel.Eq(filter))
	return s.selectInto(ctx, s.grouped(schema, q), dst)
}

// SelectTurnovers sums the movements of [from, to).
func (s *AccumulationStore) SelectTurnovers(ctx context.Context, schema *accumulation.Schema, from, to time.Time, filter accumulation.Filter, dst any) error {
	q := s.sums(schema).Where(squirrel.GtOrEq{"period": from}).Where(squirrel.Lt{"period": to}).Where(squirrel.Eq(filter))
	return s.selectInto(ctx, s.grouped(schema, q), dst)
}

// SelectRecorderTotals sums the document's movements per combination.
func (s *AccumulationStore) SelectRecorderTotals(ctx context.Context, schema *accumulation.Schema, recorderID id.ID, dst any) error {
	q := s.sums(schema).Where(squirrel.Eq{"recorder_id": recorderID}).GroupBy(quoted(schema.DimensionColumns())...)
	return s.selectInto(ctx, q, dst)
}

// SelectBalancesOf reads the balances rows of the combinations, joined with
// one array per dimension.
func (s *AccumulationStore) SelectBalancesOf(ctx context.Context, schema *accumulation.Schema, combinations [][]any, dst any) error {
	if len(combinations) == 0 {
		return nil
	}
	dimensions := quoted(schema.DimensionColumns())
	arrays := make([]string, len(schema.Dimensions))
	args := make([]any, len(schema.Dimensions))
	for i, f := range schema.Dimensions {
		values := make([]string, len(combinations))
		for j, c := range combinations {
			values[j] = fmt.Sprint(c[i])
		}
		arrays[i] = fmt.Sprintf("$%d::text[]::%s[]", i+1, f.Type.SQLType())
		args[i] = values
	}
	sql := fmt.Sprintf("SELECT %s FROM %s JOIN unnest(%s) AS k(%s) USING (%s)",
		strings.Join(quoted(schema.FieldColumns()), ", "), quote(schema.BalancesTable()),
		strings.Join(arrays, ", "), strings.Join(dimensions, ", "), strings.Join(dimensions, ", "))
	return pgxscan.Select(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), dst, sql, args...)
}

// LockCombinations takes a transaction-scoped advisory lock on each key, in
// the order given. Outside a transaction it refuses: the lock would end with
// the statement.
func (s *AccumulationStore) LockCombinations(ctx context.Context, keys []int64) error {
	if len(keys) == 0 {
		return nil
	}
	txm := postgres.MustGetTxManager(ctx)
	if txm.GetTx(ctx) == nil {
		return fmt.Errorf("register balance locks need a transaction")
	}
	batch := &pgx.Batch{}
	for _, key := range keys {
		batch.Queue("SELECT pg_advisory_xact_lock($1)", key)
	}
	results := txm.GetQuerier(ctx).SendBatch(ctx, batch)
	for range keys {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("lock combination: %w", err)
		}
	}
	return results.Close()
}

// sums selects the dimensions and the signed sum of each resource over the
// movements table.
func (s *AccumulationStore) sums(schema *accumulation.Schema) squirrel.SelectBuilder {
	columns := quoted(schema.DimensionColumns())
	for _, r := range schema.ResourceColumns() {
		c := quote(r)
		columns = append(columns, fmt.Sprintf("SUM(CASE WHEN record_type = 'receipt' THEN %s ELSE -%s END)::bigint AS %s", c, c, c))
	}
	return s.builder.Select(columns...).From(schema.MovementsTable())
}

func (s *AccumulationStore) grouped(schema *accumulation.Schema, q squirrel.SelectBuilder) squirrel.SelectBuilder {
	dimensions := quoted(schema.DimensionColumns())
	return q.GroupBy(dimensions...).Having(nonZero(schema, "SUM")).OrderBy(dimensions...)
}

// nonZero is true when any resource (or its signed sum, with aggregate
// "SUM") is not zero.
func nonZero(schema *accumulation.Schema, aggregate string) squirrel.Sqlizer {
	parts := make([]string, 0, len(schema.Resources))
	for _, r := range schema.ResourceColumns() {
		c := quote(r)
		if aggregate == "" {
			parts = append(parts, c+" <> 0")
		} else {
			parts = append(parts, fmt.Sprintf("SUM(CASE WHEN record_type = 'receipt' THEN %s ELSE -%s END) <> 0", c, c))
		}
	}
	return squirrel.Expr("(" + strings.Join(parts, " OR ") + ")")
}

func (s *AccumulationStore) selectInto(ctx context.Context, q squirrel.SelectBuilder, dst any) error {
	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}
	return pgxscan.Select(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), dst, sql, args...)
}

func quote(column string) string { return pgx.Identifier{column}.Sanitize() }

func quoted(columns []string) []string {
	result := make([]string, len(columns))
	for i, c := range columns {
		result[i] = quote(c)
	}
	return result
}

var _ accumulation.Store = (*AccumulationStore)(nil)
