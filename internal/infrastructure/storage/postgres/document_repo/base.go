// Package document_repo provides PostgreSQL implementations for document repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context per-request.
package document_repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
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
}

// NewBaseDocumentRepo creates a new base document repository.
// Note: TxManager is obtained from context, not stored in struct.
func NewBaseDocumentRepo[T any](
	tableName string,
	selectCols []string,
	newFn func() T,
) *BaseDocumentRepo[T] {
	return &BaseDocumentRepo[T]{
		tableName:  tableName,
		selectCols: selectCols,
		newFn:      newFn,
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
		Where(squirrel.Eq{"version": version})

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

// List retrieves documents with standard filtering.
func (r *BaseDocumentRepo[T]) List(ctx context.Context, filter domain.ListFilter) (domain.ListResult[T], error) {
	result := domain.ListResult[T]{
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}

	q := r.baseSelect(ctx)

	if !filter.IncludeDeleted {
		q = q.Where(squirrel.Eq{"deletion_mark": false})
	}

	if filter.Search != "" {
		q = q.Where(squirrel.ILike{"number": "%" + filter.Search + "%"})
	}

	// Count
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
	orderBy, err := r.parseOrderBy(filter.OrderBy)
	if err != nil {
		return result, err
	}
	q = q.OrderBy(orderBy)

	// Page
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

func (r *BaseDocumentRepo[T]) parseOrderBy(orderBy string) (string, error) {
	allowed := make(map[string]struct{}, len(r.selectCols)+6)
	for _, col := range r.selectCols {
		allowed[col] = struct{}{}
	}
	// Common document columns (safe even if not in selectCols for some doc types)
	allowed["id"] = struct{}{}
	allowed["number"] = struct{}{}
	allowed["date"] = struct{}{}
	allowed["created_at"] = struct{}{}
	allowed["updated_at"] = struct{}{}
	allowed["version"] = struct{}{}

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

	if _, ok := allowed[field]; !ok {
		return "", apperror.NewValidation("invalid orderBy").WithDetail("orderBy", orderBy).WithDetail("field", field)
	}

	return field + " " + direction, nil
}
