package catalog_repo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	filterPkg "github.com/alex-bogatiuk/metapus/internal/domain/filter"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	pgfilter "github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/filter"
)

// ExtensionTable stores an extension of a catalog: the fields an add-on
// declares for the catalog's items in a table of its own, one row per item,
// whose id is the item's id (PRIMARY KEY, REFERENCES the catalog's table
// ON DELETE CASCADE). The add-on's migration creates it; the catalog's table
// is never altered. An item stored before the add-on was installed has no
// row and reads as the extension's zero value.
type ExtensionTable struct {
	entityName string
	table      string
	model      reflect.Type // the extension's struct
	columns    []extensionColumn
	filter     map[string]filterPkg.FieldSpec
	search     *ExtensionSearch
}

type extensionColumn struct {
	name  string
	index []int
}

// ExtensionSearch makes the catalog's list search also find items by a
// column of the extension: the query, normalized as the column's values are,
// is a prefix of the column (an index on it serves the search).
type ExtensionSearch struct {
	Column    string
	Normalize func(string) string
}

// NewExtensionTable describes the extension table of entityName's catalog
// whose rows hold model (a struct of db-tagged fields); search, when set,
// is one of its columns.
func NewExtensionTable(entityName, table string, model reflect.Type, search *ExtensionSearch) *ExtensionTable {
	t := &ExtensionTable{entityName: entityName, table: table, model: model, search: search}
	for _, field := range reflect.VisibleFields(model) {
		column, _, _ := strings.Cut(field.Tag.Get("db"), ",")
		if field.Anonymous || !field.IsExported() || column == "" || column == "-" {
			continue
		}
		t.columns = append(t.columns, extensionColumn{name: column, index: field.Index})
	}
	columns := make([]string, len(t.columns))
	for i, c := range t.columns {
		columns[i] = c.name
	}
	t.filter = postgres.ExtractFilterFieldsOf(model, columns...)
	if search != nil {
		if _, ok := t.filter[search.Column]; !ok {
			panic(fmt.Sprintf("extension %s: search column %q is not a column of %v", table, search.Column, model))
		}
	}
	return t
}

// Table is the extension's table.
func (t *ExtensionTable) Table() string { return t.table }

// Columns are the extension's columns, without id.
func (t *ExtensionTable) Columns() []string {
	names := make([]string, len(t.columns))
	for i, c := range t.columns {
		names[i] = c.name
	}
	return names
}

// HasColumn reports whether column is one of the extension's.
func (t *ExtensionTable) HasColumn(column string) bool {
	_, ok := t.filter[column]
	return ok
}

// Write stores value, a pointer to the extension's struct, as the row of
// the item entityID, in the transaction of ctx.
func (t *ExtensionTable) Write(ctx context.Context, entityID id.ID, value any) error {
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Pointer || v.Type().Elem() != t.model || v.IsNil() {
		return apperror.NewInternal(fmt.Errorf("extension %s: value %T is not *%v", t.table, value, t.model))
	}
	v = v.Elem()
	columns := make([]string, 0, len(t.columns)+1)
	placeholders := make([]string, 0, len(t.columns)+1)
	updates := make([]string, 0, len(t.columns))
	args := make([]any, 0, len(t.columns)+1)
	columns, placeholders, args = append(columns, "id"), append(placeholders, "$1"), append(args, entityID)
	for i, c := range t.columns {
		columns = append(columns, c.name)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+2))
		updates = append(updates, c.name+" = EXCLUDED."+c.name)
		args = append(args, v.FieldByIndex(c.index).Interface())
	}
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (id) DO UPDATE SET %s",
		t.table, strings.Join(columns, ", "), strings.Join(placeholders, ", "), strings.Join(updates, ", "))
	if _, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, sql, args...); err != nil {
		return t.writeError(err)
	}
	return nil
}

func (t *ExtensionTable) writeError(err error) error {
	for _, c := range t.columns {
		if postgres.UniqueViolationOn(err, c.name) {
			return apperror.NewConflict("Значение уже занято другим элементом").WithDetail("field", c.name).WithCause(err)
		}
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" { // check_violation
		return apperror.NewValidation("Значение не подходит").WithCause(err)
	}
	if postgres.IsForeignKeyViolation(err) {
		return apperror.NewBusinessRule("INVALID_REFERENCE", "Связанный элемент удален. Выберите другой.").WithCause(err)
	}
	return fmt.Errorf("write %s: %w", t.table, err)
}

// Load reads the rows of ids, in the transaction of ctx when there is one;
// an item without a row is absent from the result.
func (t *ExtensionTable) Load(ctx context.Context, ids []id.ID) (map[id.ID]any, error) {
	result := make(map[id.ID]any, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	sql := fmt.Sprintf("SELECT id, %s FROM %s WHERE id = ANY($1)", strings.Join(t.Columns(), ", "), t.table)
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, sql, ids)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", t.table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var rowID id.ID
		value := reflect.New(t.model)
		targets := make([]any, 0, len(t.columns)+1)
		targets = append(targets, &rowID)
		for _, c := range t.columns {
			targets = append(targets, value.Elem().FieldByIndex(c.index).Addr().Interface())
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("read %s: %w", t.table, err)
		}
		result[rowID] = value.Interface()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", t.table, err)
	}
	return result, nil
}

// attach loads the extension of each item and puts it on the item: its row,
// or a zero value for an item without one.
func (t *ExtensionTable) attach(ctx context.Context, items ...entity.Extensible) error {
	ids := make([]id.ID, 0, len(items))
	for _, item := range items {
		if identified, ok := item.(interface{ GetID() id.ID }); ok {
			ids = append(ids, identified.GetID())
		}
	}
	rows, err := t.Load(ctx, ids)
	if err != nil {
		return err
	}
	for i, item := range items {
		value, ok := rows[ids[i]]
		if !ok {
			value = reflect.New(t.model).Interface()
		}
		item.ExtensionValues().Put(value)
	}
	return nil
}

// reference lets a list filter reach the extension's columns as a table one
// to one with the catalog's.
func (t *ExtensionTable) reference() filterPkg.ReferenceFieldInfo {
	return filterPkg.ReferenceFieldInfo{TableName: t.table, ForeignKey: "id", Fields: t.filter}
}

// searchCondition finds the items of parent whose search column starts with
// the normalized query; nil without a search, when the query normalizes to
// nothing, or when the reader's field policy hides the column.
func (t *ExtensionTable) searchCondition(ctx context.Context, parent, query string) squirrel.Sqlizer {
	if t.search == nil {
		return nil
	}
	if policy := security.GetFieldPolicy(ctx, t.entityName, "read"); policy != nil && !policy.IsFieldAllowed(t.search.Column) {
		return nil
	}
	normalized := strings.TrimSpace(query)
	if t.search.Normalize != nil {
		normalized = t.search.Normalize(normalized)
	}
	if normalized == "" {
		return nil
	}
	pattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(normalized) + "%"
	return squirrel.Expr(fmt.Sprintf("%s.id IN (SELECT id FROM %s WHERE %s LIKE ?)", parent, t.table, t.search.Column), pattern)
}

// RegisterExtensionTable lets the list filters and the search of the catalog
// reach the columns of an extension table (see ExtensionTable).
func (r *BaseCatalogRepo[T]) RegisterExtensionTable(table *ExtensionTable) {
	for _, column := range table.Columns() {
		if _, taken := r.filterFields[column]; taken {
			panic(fmt.Sprintf("extension %s: column %q is already a column of %s", table.table, column, r.tableName))
		}
		for _, other := range r.extensions {
			if other.HasColumn(column) {
				panic(fmt.Sprintf("extension %s: column %q is already a column of %s", table.table, column, other.table))
			}
		}
	}
	r.extensions = append(r.extensions, table)
}

// extensionOf is the extension table that has column, or nil.
func (r *BaseCatalogRepo[T]) extensionOf(column string) *ExtensionTable {
	for _, table := range r.extensions {
		if table.HasColumn(column) {
			return table
		}
	}
	return nil
}

// searchCondition is the list search: every token in the name or the code,
// or the whole query in the search column of an extension.
func (r *BaseCatalogRepo[T]) searchCondition(ctx context.Context, query string) squirrel.Sqlizer {
	condition := pgfilter.BuildSearchConditions(query, []string{"name", "code"})
	var alternatives squirrel.Or
	for _, table := range r.extensions {
		if c := table.searchCondition(ctx, r.tableName, query); c != nil {
			alternatives = append(alternatives, c)
		}
	}
	if len(alternatives) == 0 {
		return condition
	}
	if condition != nil {
		alternatives = append(squirrel.Or{condition}, alternatives...)
	}
	return alternatives
}

// extensionRegistrar is a catalog repository whose filters reach extension
// tables: BaseCatalogRepo and the repositories that embed it.
type extensionRegistrar interface {
	RegisterExtensionTable(*ExtensionTable)
}

// ExtendedRepository is a catalog repository that reads and writes an
// extension table with the catalog: in the same transaction, after the
// item's own row.
type ExtendedRepository[T entity.CatalogEntity] struct {
	inner domain.CatalogRepository[T]
	table *ExtensionTable
}

// NewExtendedRepository extends inner, whose list filters and search learn
// the extension's columns.
func NewExtendedRepository[T entity.CatalogEntity](inner domain.CatalogRepository[T], table *ExtensionTable) *ExtendedRepository[T] {
	registrar, ok := inner.(extensionRegistrar)
	if !ok {
		panic(fmt.Sprintf("extension %s: the repository %T of %s does not embed BaseCatalogRepo", table.table, inner, table.entityName))
	}
	registrar.RegisterExtensionTable(table)
	return &ExtendedRepository[T]{inner: inner, table: table}
}

var _ domain.CatalogRepository[*entity.Catalog] = (*ExtendedRepository[*entity.Catalog])(nil)

func (r *ExtendedRepository[T]) value(item T) (any, bool) {
	extensible, ok := any(item).(entity.Extensible)
	if !ok {
		return nil, false
	}
	value := extensible.ExtensionValues().Find(reflect.PointerTo(r.table.model))
	return value, value != nil
}

func (r *ExtendedRepository[T]) attach(ctx context.Context, items ...T) error {
	extensible := make([]entity.Extensible, 0, len(items))
	for _, item := range items {
		if e, ok := any(item).(entity.Extensible); ok {
			extensible = append(extensible, e)
		}
	}
	return r.table.attach(ctx, extensible...)
}

// Create stores the item, then its extension (a zero one when it has none).
func (r *ExtendedRepository[T]) Create(ctx context.Context, item T) error {
	if err := r.inner.Create(ctx, item); err != nil {
		return err
	}
	value, ok := r.value(item)
	if !ok {
		value = reflect.New(r.table.model).Interface()
		if e, isExtensible := any(item).(entity.Extensible); isExtensible {
			e.ExtensionValues().Put(value)
		}
	}
	return r.table.Write(ctx, item.GetID(), value)
}

// Update stores the item, then its extension; an item without an extension
// value keeps the stored one.
func (r *ExtendedRepository[T]) Update(ctx context.Context, item T) error {
	if err := r.inner.Update(ctx, item); err != nil {
		return err
	}
	value, ok := r.value(item)
	if !ok {
		return nil
	}
	return r.table.Write(ctx, item.GetID(), value)
}

func (r *ExtendedRepository[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
	item, err := r.inner.GetByID(ctx, entityID)
	if err != nil {
		return item, err
	}
	return item, r.attach(ctx, item)
}

func (r *ExtendedRepository[T]) GetByCode(ctx context.Context, code string) (T, error) {
	item, err := r.inner.GetByCode(ctx, code)
	if err != nil {
		return item, err
	}
	return item, r.attach(ctx, item)
}

func (r *ExtendedRepository[T]) List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[T], error) {
	result, err := r.inner.List(ctx, filter)
	if err != nil {
		return result, err
	}
	return result, r.attach(ctx, result.Items...)
}

func (r *ExtendedRepository[T]) GetTree(ctx context.Context, rootID *id.ID) ([]T, error) {
	items, err := r.inner.GetTree(ctx, rootID)
	if err != nil {
		return nil, err
	}
	return items, r.attach(ctx, items...)
}

func (r *ExtendedRepository[T]) GetPath(ctx context.Context, entityID id.ID) ([]T, error) {
	items, err := r.inner.GetPath(ctx, entityID)
	if err != nil {
		return nil, err
	}
	return items, r.attach(ctx, items...)
}

// Delete removes the item; its extension row goes with it (ON DELETE CASCADE).
func (r *ExtendedRepository[T]) Delete(ctx context.Context, entityID id.ID) error {
	return r.inner.Delete(ctx, entityID)
}

func (r *ExtendedRepository[T]) SetDeletionMark(ctx context.Context, entityID id.ID, marked bool) error {
	return r.inner.SetDeletionMark(ctx, entityID, marked)
}

func (r *ExtendedRepository[T]) Exists(ctx context.Context, entityID id.ID) (bool, error) {
	return r.inner.Exists(ctx, entityID)
}

func (r *ExtendedRepository[T]) ExistsByCode(ctx context.Context, code string) (bool, error) {
	return r.inner.ExistsByCode(ctx, code)
}

// LockHierarchy forwards the hierarchy lock of a hierarchical catalog.
func (r *ExtendedRepository[T]) LockHierarchy(ctx context.Context) error {
	locker, ok := r.inner.(domain.HierarchyLocker)
	if !ok {
		return apperror.NewInternal(fmt.Errorf("repository %T has no locking port", r.inner))
	}
	return locker.LockHierarchy(ctx)
}

// LoadStored reads the stored extension of the item, without the reader's
// field policy: the write path compares it with an edited value.
func (r *ExtendedRepository[T]) LoadStored(ctx context.Context, entityID id.ID) (any, error) {
	rows, err := r.table.Load(ctx, []id.ID{entityID})
	if err != nil {
		return nil, err
	}
	if value, ok := rows[entityID]; ok {
		return value, nil
	}
	return reflect.New(r.table.model).Interface(), nil
}
