// Package catalog_repo provides PostgreSQL implementations for catalog repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context per-request.
package catalog_repo

import (
	"context"
	"errors"
	"fmt"
	"metapus/internal/domain/cursor"
	"metapus/internal/domain/filter"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/keyset"
)

// BaseCatalogRepo provides common CRUD operations for catalog entities.
// Embed this in specific catalog repositories.
//
// In Database-per-Tenant architecture:
// - TxManager is obtained from context per-request
// - No tenant filtering in queries (isolation is physical)
type BaseCatalogRepo[T any] struct {
	tableName    string
	selectCols   []string
	newFn        func() T
	validCols       map[string]struct{}
	orderCols       map[string]struct{}
	hierarchical    bool                                 // true = supports parent_id/is_folder hierarchy
	referenceFields map[string]filter.ReferenceFieldInfo // field name → reference catalog info (for deep filtering)
}

// NewBaseCatalogRepo creates a new base catalog repository.
// Note: TxManager is obtained from context, not stored in struct.
// The hierarchical flag controls whether parent_id/is_folder are valid for filtering.
func NewBaseCatalogRepo[T any](
	tableName string,
	selectCols []string,
	newFn func() T,
	hierarchical bool,
) *BaseCatalogRepo[T] {
	validCols := make(map[string]struct{}, len(selectCols)+4)
	orderCols := make(map[string]struct{}, len(selectCols)+6)
	for _, col := range selectCols {
		validCols[col] = struct{}{}
		orderCols[col] = struct{}{}
	}
	validCols["id"] = struct{}{}
	if hierarchical {
		validCols["parent_id"] = struct{}{}
	}

	orderCols["id"] = struct{}{}
	orderCols["code"] = struct{}{}
	orderCols["name"] = struct{}{}
	orderCols["created_at"] = struct{}{}
	orderCols["updated_at"] = struct{}{}

	return &BaseCatalogRepo[T]{
		tableName:    tableName,
		selectCols:   selectCols,
		newFn:        newFn,
		validCols:    validCols,
		orderCols:    orderCols,
		hierarchical: hierarchical,
	}
}

// getTxManager retrieves TxManager from context.
// Panics if not found - this indicates a programming error (missing TenantDB middleware).
func (r *BaseCatalogRepo[T]) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// Builder returns a new squirrel builder with PostgreSQL placeholder format.
func (r *BaseCatalogRepo[T]) Builder() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

// RegisterReferenceField registers a reference field (e.g., counterpart_id)
// so that dot-notation filters like "counterpart_id.inn" are translated into
// EXISTS subqueries on the reference catalog table.
func (r *BaseCatalogRepo[T]) RegisterReferenceField(fieldName, refTableName, foreignKey string, columns []string) {
	if r.referenceFields == nil {
		r.referenceFields = make(map[string]filter.ReferenceFieldInfo)
	}
	validCols := make(map[string]struct{}, len(columns))
	for _, col := range columns {
		validCols[col] = struct{}{}
	}
	r.referenceFields[fieldName] = filter.ReferenceFieldInfo{
		TableName:  refTableName,
		ForeignKey: foreignKey,
		ValidCols:  validCols,
	}
}

// Create inserts a new entity using its "db" tags.
func (r *BaseCatalogRepo[T]) Create(ctx context.Context, entity T) error {
	data := postgres.StructToMap(entity)
	if len(data) == 0 {
		return fmt.Errorf("no db tags found in entity")
	}

	// Filter to only include columns that exist in DB
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

// Update modifies an existing entity with optimistic locking.
func (r *BaseCatalogRepo[T]) Update(ctx context.Context, entity T) error {
	data := postgres.StructToMap(entity)
	if len(data) == 0 {
		return fmt.Errorf("no db tags found in entity")
	}

	entityID, ok := data["id"]
	if !ok {
		return fmt.Errorf("entity has no 'id' field with db tag")
	}

	version, ok := data["version"].(int)
	if !ok {
		return fmt.Errorf("entity has no 'version' field or it is not an int")
	}

	// Exclude immutable fields from SET
	filteredData := make(map[string]any, len(r.selectCols))
	for _, col := range r.selectCols {
		if col == "id" {
			continue // never update ID
		}
		if col == "version" {
			continue // version is managed by repo (optimistic locking)
		}
		if val, ok := data[col]; ok {
			filteredData[col] = val
		}
	}

	q := r.Builder().
		Update(r.tableName).
		SetMap(filteredData).
		Set("version", squirrel.Expr("version + 1")).
		Where(squirrel.Eq{"id": entityID}).
		Where(squirrel.Eq{"version": version}).
		Suffix("RETURNING version")

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)

	var newVersion int
	err = querier.QueryRow(ctx, sql, args...).Scan(&newVersion)
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

	return nil
}

// baseSelect creates a SELECT builder.
func (r *BaseCatalogRepo[T]) baseSelect(ctx context.Context) squirrel.SelectBuilder {
	return r.Builder().
		Select(r.selectCols...).
		From(r.tableName)
}

// buildWhereConditions builds WHERE conditions for list filtering.
// Returns conditions that can be applied to any SelectBuilder via Where().
func (r *BaseCatalogRepo[T]) buildWhereConditions(filter domain.ListFilter) ([]squirrel.Sqlizer, error) {
	var conditions []squirrel.Sqlizer

	// Apply filters
	if !filter.IncludeDeleted {
		conditions = append(conditions, squirrel.Eq{"deletion_mark": false})
	}

	if filter.Search != "" {
		pattern := "%" + filter.Search + "%"
		conditions = append(conditions, squirrel.Or{
			squirrel.ILike{"name": pattern},
			squirrel.ILike{"code": pattern},
		})
	}

	if len(filter.IDs) > 0 {
		conditions = append(conditions, squirrel.Eq{"id": filter.IDs})
	}

	if filter.ParentID != nil && r.hierarchical {
		conditions = append(conditions, squirrel.Eq{"parent_id": *filter.ParentID})
	}

	if filter.IsFolder != nil && r.hierarchical {
		conditions = append(conditions, squirrel.Eq{"is_folder": *filter.IsFolder})
	}

	// Apply advanced filters
	advConditions, err := r.buildAdvancedFilterConditions(filter.AdvancedFilters)
	if err != nil {
		return nil, err
	}
	conditions = append(conditions, advConditions...)

	return conditions, nil
}

// GetByID retrieves entity by ID.
func (r *BaseCatalogRepo[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
	entity := r.newFn()

	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"id": entityID}).
		Limit(1)

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

// GetByCode retrieves entity by code.
func (r *BaseCatalogRepo[T]) GetByCode(ctx context.Context, code string) (T, error) {
	entity := r.newFn()

	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"code": code}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return entity, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, entity, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity, apperror.NewNotFound(r.tableName, code)
		}
		return entity, fmt.Errorf("get by code: %w", err)
	}

	return entity, nil
}

// List retrieves entities with cursor-based (keyset) pagination.
func (r *BaseCatalogRepo[T]) List(ctx context.Context, f domain.ListFilter) (domain.CursorListResult[T], error) {
	var result domain.CursorListResult[T]

	// Parse sort spec
	spec, err := keyset.ParseOrderBy(f.OrderBy, "name", "ASC", r.orderCols)
	if err != nil {
		return result, err
	}

	// Build WHERE conditions
	conditions, err := r.buildWhereConditions(f)
	if err != nil {
		return result, err
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	// Dispatch based on cursor direction
	if f.CursorReq != nil {
		switch f.CursorReq.Direction {
		case cursor.DirAround:
			return r.listAround(ctx, f, spec, conditions, limit)
		case cursor.DirAfter:
			return r.listForward(ctx, spec, conditions, f.CursorReq.Token, limit, true)
		case cursor.DirBefore:
			return r.listBackward(ctx, spec, conditions, f.CursorReq.Token, limit)
		}
	}

	// First page (no cursor) — count total + forward from start
	countQ := r.Builder().Select("COUNT(*)").From(r.tableName)
	for _, cond := range conditions {
		countQ = countQ.Where(cond)
	}
	countSQL, countArgs, err := countQ.ToSql()
	if err != nil {
		return result, fmt.Errorf("build count: %w", err)
	}
	if err := querier.QueryRow(ctx, countSQL, countArgs...).Scan(&result.TotalCount); err != nil {
		return result, fmt.Errorf("count: %w", err)
	}

	// Fetch limit+1 rows to detect hasMore
	q := r.Builder().Select(r.selectCols...).From(r.tableName)
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.OrderBy(spec.OrderByClause()).Limit(uint64(limit + 1))

	sql, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("build query: %w", err)
	}
	if err := pgxscan.Select(ctx, querier, &result.Items, sql, args...); err != nil {
		return result, fmt.Errorf("list: %w", err)
	}

	// Trim extra row and set hasMore
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.HasMore = true
	}
	result.HasPrev = false // first page

	// Build cursors
	if len(result.Items) > 0 {
		r.setCursors(&result, spec)
	}

	return result, nil
}

// listForward fetches items after a cursor (scroll down).
func (r *BaseCatalogRepo[T]) listForward(ctx context.Context, spec keyset.SortSpec, conditions []squirrel.Sqlizer, token string, limit int, setPrev bool) (domain.CursorListResult[T], error) {
	var result domain.CursorListResult[T]

	values, err := keyset.DecodeCursor(token, spec)
	if err != nil {
		return result, err
	}

	q := r.Builder().Select(r.selectCols...).From(r.tableName)
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.Where(keyset.TupleCondition(spec, values, true))
	q = q.OrderBy(spec.OrderByClause()).Limit(uint64(limit + 1))

	sql, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("build forward query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &result.Items, sql, args...); err != nil {
		return result, fmt.Errorf("list forward: %w", err)
	}

	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.HasMore = true
	}
	result.HasPrev = setPrev

	if len(result.Items) > 0 {
		r.setCursors(&result, spec)
	}
	return result, nil
}

// listBackward fetches items before a cursor (scroll up).
func (r *BaseCatalogRepo[T]) listBackward(ctx context.Context, spec keyset.SortSpec, conditions []squirrel.Sqlizer, token string, limit int) (domain.CursorListResult[T], error) {
	var result domain.CursorListResult[T]

	values, err := keyset.DecodeCursor(token, spec)
	if err != nil {
		return result, err
	}

	// Fetch in inverted order, then reverse
	q := r.Builder().Select(r.selectCols...).From(r.tableName)
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.Where(keyset.TupleCondition(spec, values, false))
	q = q.OrderBy(spec.InvertedOrderByClause()).Limit(uint64(limit + 1))

	sql, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("build backward query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &result.Items, sql, args...); err != nil {
		return result, fmt.Errorf("list backward: %w", err)
	}

	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.HasPrev = true
	}
	result.HasMore = true // we came from a cursor, so there are items after

	// Reverse to restore original sort order
	reverseSlice(result.Items)

	if len(result.Items) > 0 {
		r.setCursors(&result, spec)
	}
	return result, nil
}

// listAround fetches items around a target ID (teleportation / "show in list").
func (r *BaseCatalogRepo[T]) listAround(ctx context.Context, f domain.ListFilter, spec keyset.SortSpec, conditions []squirrel.Sqlizer, limit int) (domain.CursorListResult[T], error) {
	var result domain.CursorListResult[T]
	half := limit / 2
	if half < 1 {
		half = 1
	}

	targetID, err := id.Parse(f.CursorReq.TargetID)
	if err != nil {
		return result, apperror.NewValidation("invalid around target ID")
	}

	// 1. Get sort value of target row
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var sortValue any
	lookupSQL := fmt.Sprintf("SELECT %s FROM %s WHERE id = $1", spec.Field, r.tableName)
	if err := querier.QueryRow(ctx, lookupSQL, targetID).Scan(&sortValue); err != nil {
		return result, apperror.NewNotFound(r.tableName, targetID.String())
	}

	targetValues := []any{sortValue, targetID}

	// 2. Fetch rows BEFORE target (including target) — inverted order
	qBefore := r.Builder().Select(r.selectCols...).From(r.tableName)
	for _, cond := range conditions {
		qBefore = qBefore.Where(cond)
	}
	// >= target in inverted direction means <= in original, so we use TupleCondition(forward=false) which gives inverted op
	// Actually for "around", we need target itself + rows before it.
	// In original order (e.g. name ASC, id ASC), "before target" means (name, id) < (targetName, targetID).
	// We fetch these in inverted order (name DESC, id DESC) with no cursor condition + (name, id) >= (targetName, targetID) inverted...
	// Simpler approach: fetch rows <= target in inverted order
	qBefore = qBefore.Where(keyset.TupleConditionInclusive(spec, targetValues, false))
	qBefore = qBefore.OrderBy(spec.InvertedOrderByClause()).Limit(uint64(half + 1))

	sqlBefore, argsBefore, err := qBefore.ToSql()
	if err != nil {
		return result, fmt.Errorf("build around-before query: %w", err)
	}
	var beforeItems []T
	if err := pgxscan.Select(ctx, querier, &beforeItems, sqlBefore, argsBefore...); err != nil {
		return result, fmt.Errorf("around before: %w", err)
	}

	hasPrev := false
	if len(beforeItems) > half {
		beforeItems = beforeItems[:half]
		hasPrev = true
	}
	reverseSlice(beforeItems)

	// 3. Fetch rows AFTER target (exclusive)
	qAfter := r.Builder().Select(r.selectCols...).From(r.tableName)
	for _, cond := range conditions {
		qAfter = qAfter.Where(cond)
	}
	qAfter = qAfter.Where(keyset.TupleCondition(spec, targetValues, true))
	qAfter = qAfter.OrderBy(spec.OrderByClause()).Limit(uint64(half + 1))

	sqlAfter, argsAfter, err := qAfter.ToSql()
	if err != nil {
		return result, fmt.Errorf("build around-after query: %w", err)
	}
	var afterItems []T
	if err := pgxscan.Select(ctx, querier, &afterItems, sqlAfter, argsAfter...); err != nil {
		return result, fmt.Errorf("around after: %w", err)
	}

	hasMore := false
	if len(afterItems) > half {
		afterItems = afterItems[:half]
		hasMore = true
	}

	// 4. Merge: before (includes target) + after
	result.Items = make([]T, 0, len(beforeItems)+len(afterItems))
	result.Items = append(result.Items, beforeItems...)

	// targetIndex = last index of beforeItems (target is the last item fetched in before-set)
	targetIdx := len(beforeItems) - 1
	if targetIdx >= 0 {
		result.TargetIndex = &targetIdx
	}

	result.Items = append(result.Items, afterItems...)
	result.HasPrev = hasPrev
	result.HasMore = hasMore

	if len(result.Items) > 0 {
		r.setCursors(&result, spec)
	}

	return result, nil
}

// setCursors sets prevCursor and nextCursor on the result based on first/last items.
func (r *BaseCatalogRepo[T]) setCursors(result *domain.CursorListResult[T], spec keyset.SortSpec) {
	if len(result.Items) == 0 {
		return
	}
	first := result.Items[0]
	last := result.Items[len(result.Items)-1]

	firstMap := postgres.StructToMap(first)
	lastMap := postgres.StructToMap(last)

	if sv, ok := firstMap[spec.Field]; ok {
		if idv, ok := firstMap["id"]; ok {
			if c, err := keyset.BuildCursorFromRow(spec, sv, idv); err == nil {
				result.PrevCursor = c
			}
		}
	}
	if sv, ok := lastMap[spec.Field]; ok {
		if idv, ok := lastMap["id"]; ok {
			if c, err := keyset.BuildCursorFromRow(spec, sv, idv); err == nil {
				result.NextCursor = c
			}
		}
	}
}

// reverseSlice reverses a slice in place.
func reverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// buildAdvancedFilterConditions builds conditions from advanced filters.
// Delegates to the shared filter.BuildConditions engine and handles nested reference fields.
func (r *BaseCatalogRepo[T]) buildAdvancedFilterConditions(filters []filter.Item) ([]squirrel.Sqlizer, error) {
	if len(filters) == 0 {
		return nil, nil
	}

	var conditions []squirrel.Sqlizer
	var baseFilters []filter.Item

	for _, item := range filters {
		if strings.Contains(item.Field, ".") {
			parts := strings.SplitN(item.Field, ".", 2)
			prefix, subfield := parts[0], parts[1]

			if ref, ok := r.referenceFields[prefix]; ok {
				cond, err := filter.BuildReferenceFieldCondition(item, r.tableName, ref, subfield)
				if err != nil {
					return nil, err
				}
				conditions = append(conditions, cond)
			} else {
				return nil, fmt.Errorf("invalid dot-notation field or unknown reference prefix: %s", item.Field)
			}
		} else {
			baseFilters = append(baseFilters, item)
		}
	}

	if len(baseFilters) > 0 {
		advConditions, err := filter.BuildConditions(baseFilters, r.validCols, r.tableName)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, advConditions...)
	}

	return conditions, nil
}

// Exists checks if entity exists.
func (r *BaseCatalogRepo[T]) Exists(ctx context.Context, entityID id.ID) (bool, error) {
	q := r.Builder().
		Select("1").
		From(r.tableName).
		Where(squirrel.Eq{"id": entityID}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return false, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var exists int
	err = querier.QueryRow(ctx, sql, args...).Scan(&exists)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("exists: %w", err)
	}

	return true, nil
}

// ExistsByCode checks if entity with given code exists.
func (r *BaseCatalogRepo[T]) ExistsByCode(ctx context.Context, code string) (bool, error) {
	q := r.Builder().
		Select("1").
		From(r.tableName).
		Where(squirrel.Eq{"code": code}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return false, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var exists int
	err = querier.QueryRow(ctx, sql, args...).Scan(&exists)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("exists by code: %w", err)
	}

	return true, nil
}

// Delete performs physical removal from the database.
func (r *BaseCatalogRepo[T]) Delete(ctx context.Context, entityID id.ID) error {
	q := r.Builder().
		Delete(r.tableName).
		Where(squirrel.Eq{"id": entityID})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	result, err := r.getTxManager(ctx).GetQuerier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		// Check for foreign key violation (23503)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return apperror.NewConflict("Нельзя удалить: объект используется в других документах или справочниках").
				WithDetail("entity", r.tableName).
				WithDetail("id", entityID.String()).
				WithCause(err)
		}
		return fmt.Errorf("execute delete %s: %w", r.tableName, err)
	}

	if result.RowsAffected() == 0 {
		return apperror.NewNotFound(r.tableName, entityID.String())
	}

	return nil
}

// SetDeletionMark sets or clears the deletion mark (soft delete).
func (r *BaseCatalogRepo[T]) SetDeletionMark(ctx context.Context, entityID id.ID, marked bool) error {
	q := r.Builder().
		Update(r.tableName).
		Set("deletion_mark", marked).
		Set("version", squirrel.Expr("version + 1")).
		Where(squirrel.Eq{"id": entityID})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build set deletion mark: %w", err)
	}

	result, err := r.getTxManager(ctx).GetQuerier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("execute set deletion mark: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apperror.NewNotFound(r.tableName, entityID.String())
	}

	return nil
}

// GetTree retrieves hierarchical structure using recursive CTE.
// Returns error if the catalog is not hierarchical.
func (r *BaseCatalogRepo[T]) GetTree(ctx context.Context, rootID *id.ID) ([]T, error) {
	if !r.hierarchical {
		return nil, fmt.Errorf("GetTree is not supported for non-hierarchical catalogs")
	}

	var items []T

	rootCond, rootArgs := r.rootCondition(rootID)
	args := rootArgs

	cteCols := strings.Join(r.treeSelectCols(), ", ")
	cteSQL := fmt.Sprintf(`
		WITH RECURSIVE tree AS (
			SELECT %s, 0 as level
			FROM %s
			WHERE %s AND deletion_mark = false
			
			UNION ALL
			
			SELECT %s, t.level + 1
			FROM %s c
			INNER JOIN tree t ON c.parent_id = t.id
			WHERE c.deletion_mark = false
		)
		SELECT %s FROM tree
		ORDER BY level, name
	`, cteCols, r.tableName, rootCond, cteCols, r.tableName, strings.Join(r.selectCols, ", "))

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &items, cteSQL, args...); err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	return items, nil
}

// GetPath retrieves path from root to entity.
// Returns error if the catalog is not hierarchical.
func (r *BaseCatalogRepo[T]) GetPath(ctx context.Context, entityID id.ID) ([]T, error) {
	if !r.hierarchical {
		return nil, fmt.Errorf("GetPath is not supported for non-hierarchical catalogs")
	}

	var items []T

	args := []any{entityID}

	cteCols := strings.Join(r.pathSelectCols(), ", ")
	cteSQL := fmt.Sprintf(`
		WITH RECURSIVE path AS (
			SELECT %s, 0 as level
			FROM %s
			WHERE id = $1
			
			UNION ALL
			
			SELECT %s, p.level + 1
			FROM %s c
			INNER JOIN path p ON c.id = p.parent_id
		)
		SELECT %s FROM path
		ORDER BY level DESC
	`, cteCols, r.tableName, cteCols, r.tableName, strings.Join(r.selectCols, ", "))

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &items, cteSQL, args...); err != nil {
		return nil, fmt.Errorf("get path: %w", err)
	}

	return items, nil
}

// GetForUpdate retrieves entity by ID with row lock.
func (r *BaseCatalogRepo[T]) GetForUpdate(ctx context.Context, entityID id.ID) (T, error) {
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

// FindOne executes a SELECT query and returns a single entity.
func (r *BaseCatalogRepo[T]) FindOne(ctx context.Context, q squirrel.SelectBuilder) (T, error) {
	entity := r.newFn()

	sql, args, err := q.ToSql()
	if err != nil {
		return entity, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, entity, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity, apperror.NewNotFound(r.tableName, "matching query")
		}
		return entity, fmt.Errorf("find one: %w", err)
	}

	return entity, nil
}

// Helper methods

func (r *BaseCatalogRepo[T]) rootCondition(rootID *id.ID) (string, []any) {
	if rootID == nil {
		return "parent_id IS NULL", nil
	}
	return "parent_id = $1", []any{*rootID}
}

func (r *BaseCatalogRepo[T]) treeSelectCols() []string {
	cols := make([]string, 0, len(r.selectCols)+2)
	cols = append(cols, "id", "parent_id", "deletion_mark")
	cols = append(cols, r.selectCols...)
	return uniqueCols(cols)
}

func (r *BaseCatalogRepo[T]) pathSelectCols() []string {
	cols := make([]string, 0, len(r.selectCols)+2)
	cols = append(cols, "id", "parent_id")
	cols = append(cols, r.selectCols...)
	return uniqueCols(cols)
}

func uniqueCols(cols []string) []string {
	seen := make(map[string]struct{}, len(cols))
	out := make([]string, 0, len(cols))
	for _, col := range cols {
		if _, ok := seen[col]; ok {
			continue
		}
		seen[col] = struct{}{}
		out = append(out, col)
	}
	return out
}
