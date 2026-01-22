// Package catalog_repo provides PostgreSQL implementations for catalog repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context per-request.
package catalog_repo

import (
	"context"
	"errors"
	"fmt"
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
)

// BaseCatalogRepo provides common CRUD operations for catalog entities.
// Embed this in specific catalog repositories.
//
// In Database-per-Tenant architecture:
// - TxManager is obtained from context per-request
// - No tenant filtering in queries (isolation is physical)
type BaseCatalogRepo[T any] struct {
	tableName  string
	selectCols []string
	newFn      func() T
}

// NewBaseCatalogRepo creates a new base catalog repository.
// Note: TxManager is obtained from context, not stored in struct.
func NewBaseCatalogRepo[T any](
	tableName string,
	selectCols []string,
	newFn func() T,
) *BaseCatalogRepo[T] {
	return &BaseCatalogRepo[T]{
		tableName:  tableName,
		selectCols: selectCols,
		newFn:      newFn,
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
		Where(squirrel.Eq{"version": version}) // optimistic lock: expect current version

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	result, err := querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update %s: %w", r.tableName, err)
	}

	if result.RowsAffected() == 0 {
		return apperror.NewConcurrentModification(r.tableName, entityID)
	}

	return nil
}

// baseSelect creates a SELECT builder.
func (r *BaseCatalogRepo[T]) baseSelect(ctx context.Context) squirrel.SelectBuilder {
	return r.Builder().
		Select(r.selectCols...).
		From(r.tableName)
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

// List retrieves entities with filtering and pagination.
func (r *BaseCatalogRepo[T]) List(ctx context.Context, filter domain.ListFilter) (domain.ListResult[T], error) {
	result := domain.ListResult[T]{
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}

	// Build base query
	q := r.baseSelect(ctx)

	// Apply filters
	if !filter.IncludeDeleted {
		q = q.Where(squirrel.Eq{"deletion_mark": false})
	}

	if filter.Search != "" {
		pattern := "%" + filter.Search + "%"
		q = q.Where(squirrel.Or{
			squirrel.ILike{"name": pattern},
			squirrel.ILike{"code": pattern},
		})
	}

	if len(filter.IDs) > 0 {
		q = q.Where(squirrel.Eq{"id": filter.IDs})
	}

	if filter.ParentID != nil {
		q = q.Where(squirrel.Eq{"parent_id": *filter.ParentID})
	}

	if filter.IsFolder != nil {
		q = q.Where(squirrel.Eq{"is_folder": *filter.IsFolder})
	}

	// Apply advanced filters
	var err error
	q, err = r.applyAdvancedFilters(ctx, q, filter.AdvancedFilters)
	if err != nil {
		return domain.ListResult[T]{}, err
	}

	// Count total (before pagination)
	countQ := r.Builder().
		Select("COUNT(*)").
		FromSelect(q, "sub")

	countSQL, countArgs, err := countQ.ToSql()
	if err != nil {
		return result, fmt.Errorf("build count query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := querier.QueryRow(ctx, countSQL, countArgs...).Scan(&result.TotalCount); err != nil {
		return result, fmt.Errorf("count: %w", err)
	}

	// Apply ordering
	orderBy, err := r.parseOrderBy(filter.OrderBy)
	if err != nil {
		return result, err
	}
	q = q.OrderBy(orderBy)

	// Apply pagination
	if filter.Limit > 0 {
		q = q.Limit(uint64(filter.Limit))
	}
	if filter.Offset > 0 {
		q = q.Offset(uint64(filter.Offset))
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

// applyAdvancedFilters applies complex filters to query.
func (r *BaseCatalogRepo[T]) applyAdvancedFilters(ctx context.Context, q squirrel.SelectBuilder, filters []filter.Item) (squirrel.SelectBuilder, error) {
	// Whitelist columns for SQL injection protection
	validCols := make(map[string]bool, len(r.selectCols))
	for _, col := range r.selectCols {
		validCols[col] = true
	}
	validCols["id"] = true
	validCols["parent_id"] = true

	for _, item := range filters {
		if !validCols[item.Field] {
			return q, fmt.Errorf("invalid filter column: %s", item.Field)
		}

		switch item.Operator {
		case filter.Equal:
			q = q.Where(squirrel.Eq{item.Field: item.Value})
		case filter.NotEqual:
			q = q.Where(squirrel.NotEq{item.Field: item.Value})
		case filter.LessOrEqual:
			q = q.Where(squirrel.LtOrEq{item.Field: item.Value})
		case filter.GreaterOrEqual:
			q = q.Where(squirrel.GtOrEq{item.Field: item.Value})
		case filter.Less:
			q = q.Where(squirrel.Lt{item.Field: item.Value})
		case filter.Greater:
			q = q.Where(squirrel.Gt{item.Field: item.Value})
		case filter.InList:
			q = q.Where(squirrel.Eq{item.Field: item.Value})
		case filter.NotInList:
			q = q.Where(squirrel.NotEq{item.Field: item.Value})
		case filter.IsNull:
			q = q.Where(squirrel.Eq{item.Field: nil})
		case filter.IsNotNull:
			q = q.Where(squirrel.NotEq{item.Field: nil})
		case filter.Contains:
			val := fmt.Sprintf("%%%v%%", item.Value)
			q = q.Where(squirrel.ILike{item.Field: val})
		case filter.NotContains:
			val := fmt.Sprintf("%%%v%%", item.Value)
			q = q.Where(squirrel.NotILike{item.Field: val})
		case filter.InHierarchy:
			cteSQL := fmt.Sprintf(`
                id IN (
                    WITH RECURSIVE hierarchy AS (
                        SELECT id FROM %s WHERE id = ? 
                        UNION ALL 
                        SELECT t.id FROM %s t JOIN hierarchy h ON t.parent_id = h.id
                    ) 
                    SELECT id FROM hierarchy
                )
            `, r.tableName, r.tableName)
			q = q.Where(squirrel.Expr(cteSQL, item.Value))
		case filter.NotInHierarchy:
			cteSQL := fmt.Sprintf(`
                id NOT IN (
                    WITH RECURSIVE hierarchy AS (
                        SELECT id FROM %s WHERE id = ? 
                        UNION ALL 
                        SELECT t.id FROM %s t JOIN hierarchy h ON t.parent_id = h.id
                    ) 
                    SELECT id FROM hierarchy
                )
            `, r.tableName, r.tableName)
			q = q.Where(squirrel.Expr(cteSQL, item.Value))
		}
	}

	return q, nil
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
func (r *BaseCatalogRepo[T]) GetTree(ctx context.Context, rootID *id.ID) ([]T, error) {
	var items []T

	rootCond, rootArgs := r.rootCondition(rootID)
	args := rootArgs

	cteSQL := fmt.Sprintf(`
		WITH RECURSIVE tree AS (
			SELECT *, 0 as level
			FROM %s
			WHERE %s AND deletion_mark = false
			
			UNION ALL
			
			SELECT c.*, t.level + 1
			FROM %s c
			INNER JOIN tree t ON c.parent_id = t.id
			WHERE c.deletion_mark = false
		)
		SELECT %s FROM tree
		ORDER BY level, name
	`, r.tableName, rootCond, r.tableName, strings.Join(r.selectCols, ", "))

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &items, cteSQL, args...); err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	return items, nil
}

// GetPath retrieves path from root to entity.
func (r *BaseCatalogRepo[T]) GetPath(ctx context.Context, entityID id.ID) ([]T, error) {
	var items []T

	args := []any{entityID}

	cteSQL := fmt.Sprintf(`
		WITH RECURSIVE path AS (
			SELECT *, 0 as level
			FROM %s
			WHERE id = $1
			
			UNION ALL
			
			SELECT c.*, p.level + 1
			FROM %s c
			INNER JOIN path p ON c.id = p.parent_id
		)
		SELECT %s FROM path
		ORDER BY level DESC
	`, r.tableName, r.tableName, strings.Join(r.selectCols, ", "))

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

func (r *BaseCatalogRepo[T]) parseOrderBy(orderBy string) (string, error) {
	allowed := make(map[string]struct{}, len(r.selectCols)+4)
	for _, col := range r.selectCols {
		allowed[col] = struct{}{}
	}
	// Common catalog columns (safe even if not in selectCols for some types)
	allowed["id"] = struct{}{}
	allowed["code"] = struct{}{}
	allowed["name"] = struct{}{}
	allowed["created_at"] = struct{}{}
	allowed["updated_at"] = struct{}{}

	if orderBy == "" {
		return "name ASC", nil
	}

	// Support "-field" for DESC.
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

	if _, ok := allowed[field]; !ok {
		return "", apperror.NewValidation("invalid orderBy").WithDetail("orderBy", orderBy).WithDetail("field", field)
	}

	return field + " " + direction, nil
}
