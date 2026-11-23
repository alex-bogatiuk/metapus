package postgres

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/keyset"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ResolveSortSpec takes type/nullability from the actual tenant schema. A Go
// zero UUID or empty string does not prove that its SQL column is NOT NULL.
func ResolveSortSpec(ctx context.Context, querier Querier, table string, spec keyset.SortSpec) (keyset.SortSpec, error) {
	err := querier.QueryRow(ctx, `SELECT t.typname, NOT a.attnotnull FROM pg_attribute a JOIN pg_type t ON t.oid=a.atttypid WHERE a.attrelid=$1::regclass AND a.attname=$2 AND a.attnum>0 AND NOT a.attisdropped`, table, spec.Field).Scan(&spec.FieldType, &spec.Nullable)
	if err != nil {
		return spec, fmt.Errorf("resolve sort column: %w", err)
	}
	if !keyset.SupportedType(spec.FieldType) {
		return spec, apperror.NewValidation("unsupported sort field type").WithDetail("field", spec.Field)
	}
	return spec, nil
}

// cursorScanRows hides only the final internal sort projection from scany.
// The SQL value is read in the same row/snapshot as the entity, before any
// model codec can turn NULL into a zero UUID or round a numeric value.
type cursorScanRows struct {
	pgx.Rows
	value *string
}

func (r *cursorScanRows) FieldDescriptions() []pgconn.FieldDescription {
	fields := r.Rows.FieldDescriptions()
	return fields[:len(fields)-1]
}
func (r *cursorScanRows) Scan(dest ...any) error { return r.Rows.Scan(append(dest, &r.value)...) }

// ScanCursorPage expects one trailing column: sort_field::text AS __cursor_sort.
func ScanCursorPage[T any](ctx context.Context, querier Querier, newFn func() T, spec keyset.SortSpec, query string, args ...any) ([]T, map[id.ID]string, error) {
	rows, err := querier.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	if len(fields) == 0 || fields[len(fields)-1].Name != "__cursor_sort" {
		return nil, nil, fmt.Errorf("missing cursor sort projection")
	}
	wrapper := &cursorScanRows{Rows: rows}
	scanner := pgxscan.NewRowScanner(wrapper)
	items := make([]T, 0)
	cursors := map[id.ID]string{}
	for rows.Next() {
		item := newFn()
		if err := scanner.Scan(item); err != nil {
			return nil, nil, err
		}
		identified, ok := any(item).(interface{ GetID() id.ID })
		if !ok {
			return nil, nil, fmt.Errorf("cursor row has no identity")
		}
		token, err := keyset.BuildCursorFromRow(spec, wrapper.value, identified.GetID())
		if err != nil {
			return nil, nil, err
		}
		cursors[identified.GetID()] = token
		items = append(items, item)
	}
	return items, cursors, rows.Err()
}

func ApplyPageCursors[T any](result *domain.CursorListResult[T], cursors map[id.ID]string) error {
	if len(result.Items) == 0 {
		return nil
	}
	first, ok := any(result.Items[0]).(interface{ GetID() id.ID })
	if !ok {
		return fmt.Errorf("cursor row has no identity")
	}
	last := any(result.Items[len(result.Items)-1]).(interface{ GetID() id.ID })
	result.PrevCursor, result.NextCursor = cursors[first.GetID()], cursors[last.GetID()]
	if result.PrevCursor == "" || result.NextCursor == "" {
		return fmt.Errorf("cursor boundary was not captured")
	}
	return nil
}
