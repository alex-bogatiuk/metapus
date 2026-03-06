// Package document_repo provides PostgreSQL implementations for document repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context per-request.
package document_repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/filter"
	"metapus/internal/infrastructure/storage/postgres"
)

// BaseDocumentRepo provides common CRUD operations for document entities.
// In Database-per-Tenant architecture:
// - TxManager is obtained from context per-request
// - No tenant filtering in queries (isolation is physical)
type BaseDocumentRepo[T any] struct {
	tableName  string
	selectCols []string
	newFn      func() T
	validCols  map[string]struct{}             // whitelist for advanced filter columns
	orderCols  map[string]struct{}             // whitelist for ORDER BY columns
	tableParts map[string]filter.TablePartInfo // table part name → child table info (for EXISTS subqueries)
}

// NewBaseDocumentRepo creates a new base document repository.
// Note: TxManager is obtained from context, not stored in struct.
// Automatically builds validCols and orderCols from selectCols
// plus standard document columns.
func NewBaseDocumentRepo[T any](
	tableName string,
	selectCols []string,
	newFn func() T,
) *BaseDocumentRepo[T] {
	// Standard document columns always valid for filtering
	extraFilterCols := []string{"id", "number", "date", "posted", "deletion_mark"}
	validCols := filter.BuildValidCols(selectCols, extraFilterCols...)

	// Standard document columns always valid for ordering
	extraOrderCols := []string{"id", "number", "date", "created_at", "updated_at", "version"}
	orderCols := filter.BuildOrderCols(selectCols, extraOrderCols...)

	return &BaseDocumentRepo[T]{
		tableName:  tableName,
		selectCols: selectCols,
		newFn:      newFn,
		validCols:  validCols,
		orderCols:  orderCols,
	}
}

// RegisterTablePart registers a child table (table part / tabular section)
// so that dot-notation filters like "lines.product_id" are translated into
// EXISTS subqueries instead of direct WHERE conditions on the main table.
//
// partName is the snake_case name that arrives from frontend (e.g. "lines").
// childTable is the SQL table name (e.g. "doc_goods_receipt_lines").
// foreignKey is the FK column in the child table (e.g. "document_id").
// columns are the DB column names allowed for filtering.
func (r *BaseDocumentRepo[T]) RegisterTablePart(partName, childTable, foreignKey string, columns []string) {
	if r.tableParts == nil {
		r.tableParts = make(map[string]filter.TablePartInfo)
	}
	validCols := make(map[string]struct{}, len(columns))
	for _, col := range columns {
		validCols[col] = struct{}{}
	}
	r.tableParts[partName] = filter.TablePartInfo{
		TableName:  childTable,
		ForeignKey: foreignKey,
		ValidCols:  validCols,
	}
}

// getTxManager retrieves TxManager from context.
func (r *BaseDocumentRepo[T]) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// Builder returns a new squirrel builder.
func (r *BaseDocumentRepo[T]) Builder() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

// Create inserts a new document.
func (r *BaseDocumentRepo[T]) Create(ctx context.Context, entity T) error {
	data := postgres.StructToMap(entity)
	if len(data) == 0 {
		return fmt.Errorf("no db tags found in entity")
	}

	filteredData := make(map[string]any, len(r.selectCols))
	for _, col := range r.selectCols {
		if val, ok := data[col]; ok {
			filteredData[col] = val
		}
	}

	q := r.Builder().
		Insert(r.tableName).
		SetMap(filteredData)

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	_, err = querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("insert %s: %w", r.tableName, err)
	}

	return nil
}

// Update updates an existing document with optimistic locking.
func (r *BaseDocumentRepo[T]) Update(ctx context.Context, entity T) error {
	data := postgres.StructToMap(entity)
	if len(data) == 0 {
		return fmt.Errorf("no db tags found in entity")
	}

	entityID, ok := data["id"]
	if !ok {
		return fmt.Errorf("entity has no 'id' field")
	}

	version, ok := data["version"].(int)
	if !ok {
		return fmt.Errorf("entity has no 'version' field or it is not an int")
	}

	// Exclude immutable fields
	filteredData := make(map[string]any, len(r.selectCols))
	for _, col := range r.selectCols {
		if col == "id" || col == "created_at" || col == "created_by" {
			continue
		}
		if col == "version" || col == "updated_at" {
			continue // version/updated_at are managed by repo
		}
		if val, ok := data[col]; ok {
			filteredData[col] = val
		}
	}

	q := r.Builder().
		Update(r.tableName).
		SetMap(filteredData).
		Set("version", squirrel.Expr("version + 1")).
		Set("updated_at", squirrel.Expr("NOW()")).
		Where(squirrel.Eq{"id": entityID}).
		Where(squirrel.Eq{"version": version}).
		Suffix("RETURNING version, updated_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)

	var newVersion int
	var updatedAt time.Time

	err = querier.QueryRow(ctx, sql, args...).Scan(&newVersion, &updatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return apperror.NewConcurrentModification(r.tableName, entityID)
		}
		return fmt.Errorf("update %s: %w", r.tableName, err)
	}

	// Update entity in memory to prevent stale object issues
	if v, ok := any(entity).(interface{ SetVersion(int) }); ok {
		v.SetVersion(newVersion)
	}
	if v, ok := any(entity).(interface{ SetUpdatedAt(time.Time) }); ok {
		v.SetUpdatedAt(updatedAt)
	}

	return nil
}

// Delete soft-deletes a document.
func (r *BaseDocumentRepo[T]) Delete(ctx context.Context, entityID id.ID) error {
	q := r.Builder().
		Update(r.tableName).
		Set("deletion_mark", true).
		Set("updated_at", squirrel.Expr("NOW()")).
		Set("version", squirrel.Expr("version + 1")).
		Where(squirrel.Eq{"id": entityID})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	result, err := querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("delete %s: %w", r.tableName, err)
	}

	if result.RowsAffected() == 0 {
		return apperror.NewNotFound(r.tableName, entityID.String())
	}

	return nil
}

// baseSelect creates a SELECT builder.
func (r *BaseDocumentRepo[T]) baseSelect(ctx context.Context) squirrel.SelectBuilder {
	return r.Builder().
		Select(r.selectCols...).
		From(r.tableName)
}

// GetByID retrieves a document by ID.
func (r *BaseDocumentRepo[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
	entity := r.newFn()
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"id": entityID})

	sql, args, err := q.ToSql()
	if err != nil {
		return entity, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, entity, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity, apperror.NewNotFound(r.tableName, entityID.String())
		}
		return entity, fmt.Errorf("get by id: %w", err)
	}

	return entity, nil
}

// GetByNumber retrieves a document by Number.
func (r *BaseDocumentRepo[T]) GetByNumber(ctx context.Context, number string) (T, error) {
	entity := r.newFn()
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"number": number})

	sql, args, err := q.ToSql()
	if err != nil {
		return entity, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, entity, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity, apperror.NewNotFound(r.tableName, number)
		}
		return entity, fmt.Errorf("get by number: %w", err)
	}

	return entity, nil
}

// GetForUpdate retrieves document with row lock.
func (r *BaseDocumentRepo[T]) GetForUpdate(ctx context.Context, entityID id.ID) (T, error) {
	entity := r.newFn()
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"id": entityID}).
		Suffix("FOR UPDATE")

	sql, args, err := q.ToSql()
	if err != nil {
		return entity, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, entity, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity, apperror.NewNotFound(r.tableName, entityID.String())
		}
		return entity, fmt.Errorf("get for update: %w", err)
	}

	return entity, nil
}

// buildWhereConditions builds WHERE conditions from domain.ListFilter.
// Handles standard filters (search, deletion_mark) and advanced filters.
func (r *BaseDocumentRepo[T]) buildWhereConditions(f domain.ListFilter) ([]squirrel.Sqlizer, error) {
	var conditions []squirrel.Sqlizer

	if !f.IncludeDeleted {
		conditions = append(conditions, squirrel.Eq{"deletion_mark": false})
	}

	if f.Search != "" {
		conditions = append(conditions, squirrel.ILike{"number": "%" + f.Search + "%"})
	}

	// Apply advanced filters: separate header filters from table-part filters (dot notation)
	if len(f.AdvancedFilters) > 0 {
		var headerFilters []filter.Item
		for _, item := range f.AdvancedFilters {
			if strings.Contains(item.Field, ".") {
				// Table part filter → EXISTS subquery
				cond, err := r.buildTablePartCondition(item)
				if err != nil {
					return nil, err
				}
				conditions = append(conditions, cond)
			} else {
				headerFilters = append(headerFilters, item)
			}
		}
		if len(headerFilters) > 0 {
			advConditions, err := filter.BuildConditions(headerFilters, r.validCols, r.tableName)
			if err != nil {
				return nil, err
			}
			conditions = append(conditions, advConditions...)
		}
	}

	return conditions, nil
}

// List retrieves documents with standard filtering and advanced filters.
func (r *BaseDocumentRepo[T]) List(ctx context.Context, f domain.ListFilter) (domain.ListResult[T], error) {
	result := domain.ListResult[T]{
		Limit:  f.Limit,
		Offset: f.Offset,
	}

	// Build WHERE conditions
	conditions, err := r.buildWhereConditions(f)
	if err != nil {
		return result, err
	}

	// Build base SELECT and apply conditions
	q := r.baseSelect(ctx)
	for _, cond := range conditions {
		q = q.Where(cond)
	}

	// Count total (before pagination)
	countQ := r.Builder().Select("COUNT(*)").FromSelect(q, "sub")
	countSQL, countArgs, err := countQ.ToSql()
	if err != nil {
		return result, fmt.Errorf("build count: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := querier.QueryRow(ctx, countSQL, countArgs...).Scan(&result.TotalCount); err != nil {
		return result, fmt.Errorf("count: %w", err)
	}

	// Order
	orderBy, err := r.parseOrderBy(f.OrderBy)
	if err != nil {
		return result, err
	}
	q = q.OrderBy(orderBy)

	// Page
	if f.Limit > 0 {
		q = q.Limit(uint64(f.Limit))
	}
	if f.Offset > 0 {
		q = q.Offset(uint64(f.Offset))
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("build query: %w", err)
	}

	if err := pgxscan.Select(ctx, querier, &result.Items, sql, args...); err != nil {
		return result, fmt.Errorf("list: %w", err)
	}

	return result, nil
}

// buildTablePartCondition resolves a dot-notation filter field (e.g. "lines.product_id")
// into an EXISTS subquery against the registered child table.
func (r *BaseDocumentRepo[T]) buildTablePartCondition(item filter.Item) (squirrel.Sqlizer, error) {
	parts := strings.SplitN(item.Field, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid table part filter field: %s", item.Field)
	}

	partName := parts[0]
	columnName := parts[1]

	tp, ok := r.tableParts[partName]
	if !ok {
		return nil, fmt.Errorf("unknown table part: %s", partName)
	}

	return filter.BuildTablePartCondition(item, r.tableName, tp, columnName)
}

func (r *BaseDocumentRepo[T]) parseOrderBy(orderBy string) (string, error) {
	if strings.TrimSpace(orderBy) == "" {
		return "date DESC", nil
	}

	direction := "ASC"
	field := orderBy
	if strings.HasPrefix(orderBy, "-") {
		direction = "DESC"
		field = strings.TrimPrefix(orderBy, "-")
	} else if strings.HasPrefix(orderBy, "+") {
		field = strings.TrimPrefix(orderBy, "+")
	}

	field = strings.TrimSpace(field)
	if field == "" {
		return "", apperror.NewValidation("invalid orderBy").WithDetail("orderBy", orderBy)
	}

	if _, ok := r.orderCols[field]; !ok {
		return "", apperror.NewValidation("invalid orderBy").WithDetail("orderBy", orderBy).WithDetail("field", field)
	}

	return field + " " + direction, nil
}
