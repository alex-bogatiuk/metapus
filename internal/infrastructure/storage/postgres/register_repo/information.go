package register_repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/information"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// InformationStore keeps the records of declarative information registers
// in the tables their schemas name. It holds no state: the transaction comes
// from the context.
type InformationStore struct {
	builder squirrel.StatementBuilderType
}

// NewInformationStore creates the store.
func NewInformationStore() *InformationStore {
	return &InformationStore{builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)}
}

// Upsert writes the record, replacing the one with its key.
func (s *InformationStore) Upsert(ctx context.Context, schema *information.Schema, record information.Record) error {
	columns := schema.Columns()
	updates := make([]string, 0, len(schema.Resources)+2)
	for _, c := range append(schema.ResourceColumns(), headerUpdates(schema)...) {
		updates = append(updates, fmt.Sprintf("%s = EXCLUDED.%s", quote(c), quote(c)))
	}
	q := s.builder.Insert(quote(schema.Table)).Columns(quoted(columns)...).Values(schema.Values(record)...).
		Suffix(fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s", strings.Join(quoted(schema.KeyColumns()), ", "), strings.Join(updates, ", ")))
	return s.exec(ctx, q)
}

// headerUpdates are the header columns a replaced record takes over besides
// its key: none but the recorder, which an Independent register has not.
func headerUpdates(schema *information.Schema) []string {
	if schema.Mode == information.Recorder {
		return []string{"recorder_id", "recorder_type"}
	}
	return nil
}

// DeleteKey removes the record with the key of record.
func (s *InformationStore) DeleteKey(ctx context.Context, schema *information.Schema, record information.Record) error {
	eq := squirrel.Eq{}
	for i, value := range schema.KeyValues(record) {
		eq[quote(schema.KeyColumns()[i])] = value
	}
	return s.exec(ctx, s.builder.Delete(quote(schema.Table)).Where(eq))
}

// InsertRecords writes a document's records; a key another record holds is
// a conflict the user sees.
func (s *InformationStore) InsertRecords(ctx context.Context, schema *information.Schema, records []information.Record) error {
	if len(records) == 0 {
		return nil
	}
	q := s.builder.Insert(quote(schema.Table)).Columns(quoted(schema.Columns())...)
	for _, r := range records {
		q = q.Values(schema.Values(r)...)
	}
	err := s.exec(ctx, q)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperror.NewBusinessRule(apperror.CodeDuplicate,
			fmt.Sprintf("«%s» already has a record for this period and these dimensions", schema.Label)).WithCause(err)
	}
	return err
}

// DeleteRecorder removes the records of a document.
func (s *InformationStore) DeleteRecorder(ctx context.Context, schema *information.Schema, recorderID id.ID) error {
	return s.exec(ctx, s.builder.Delete(quote(schema.Table)).Where(squirrel.Eq{"recorder_id": recorderID}))
}

// SelectSliceLast reads the latest record at or before at per combination.
func (s *InformationStore) SelectSliceLast(ctx context.Context, schema *information.Schema, at time.Time, filter information.Filter, dst any) error {
	dims := quoted(schema.DimensionColumns())
	q := s.builder.Select(quoted(schema.Columns())...).From(quote(schema.Table)).Where(squirrel.Eq(filter))
	if !schema.Periodic() {
		return s.selectInto(ctx, q.OrderBy(dims...), dst)
	}
	q = q.Where(squirrel.LtOrEq{"period": schema.PeriodOf(at)})
	if len(dims) == 0 {
		return s.selectInto(ctx, q.OrderBy("period DESC").Limit(1), dst)
	}
	q = q.Options("DISTINCT ON (" + strings.Join(dims, ", ") + ")").OrderBy(append(dims, "period DESC")...)
	return s.selectInto(ctx, q, dst)
}

// SelectRecords reads the records the filter selects in [from, to].
func (s *InformationStore) SelectRecords(ctx context.Context, schema *information.Schema, filter information.Filter, from, to time.Time, dst any) error {
	q := s.builder.Select(quoted(schema.Columns())...).From(quote(schema.Table)).Where(squirrel.Eq(filter))
	if schema.Periodic() {
		if !from.IsZero() {
			q = q.Where(squirrel.GtOrEq{"period": schema.PeriodOf(from)})
		}
		if !to.IsZero() {
			q = q.Where(squirrel.LtOrEq{"period": to})
		}
	}
	return s.selectInto(ctx, q.OrderBy(quoted(schema.KeyColumns())...), dst)
}

// SelectRecorder reads a document's records.
func (s *InformationStore) SelectRecorder(ctx context.Context, schema *information.Schema, recorderID id.ID, dst any) error {
	q := s.builder.Select(quoted(schema.Columns())...).From(quote(schema.Table)).
		Where(squirrel.Eq{"recorder_id": recorderID}).OrderBy(quoted(schema.KeyColumns())...)
	return s.selectInto(ctx, q, dst)
}

func (s *InformationStore) exec(ctx context.Context, q squirrel.Sqlizer) error {
	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build statement: %w", err)
	}
	_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, sql, args...)
	return err
}

func (s *InformationStore) selectInto(ctx context.Context, q squirrel.SelectBuilder, dst any) error {
	return (&AccumulationStore{builder: s.builder}).selectInto(ctx, q, dst)
}

var _ information.Store = (*InformationStore)(nil)
