package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/listview"
)

// ListViewRepo implements listview.Repository.
type ListViewRepo struct{}

// NewListViewRepo creates a new list view repository.
func NewListViewRepo() *ListViewRepo {
	return &ListViewRepo{}
}

func (r *ListViewRepo) psql() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

var listViewColumns = []string{
	"id", "entity_type", "name", "author_id", "visibility",
	"is_default", "sort_order", "config",
	"deletion_mark", "version", "created_at", "updated_at",
}

func scanListView(row pgx.Row, v *listview.ListView) error {
	var configJSON []byte
	err := row.Scan(
		&v.ID, &v.EntityType, &v.Name, &v.AuthorID, &v.Visibility,
		&v.IsDefault, &v.SortOrder, &configJSON,
		&v.DeletionMark, &v.Version, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(configJSON, &v.Config); err != nil {
		return apperror.NewInternal(fmt.Errorf("unmarshal list view config: %w", err))
	}

	// Initialize nil slices to empty.
	if v.Config.Columns == nil {
		v.Config.Columns = make([]string, 0)
	}
	if v.Config.Filters == nil {
		v.Config.Filters = json.RawMessage("{}")
	}

	return nil
}

// Create inserts a new list view.
func (r *ListViewRepo) Create(ctx context.Context, v *listview.ListView) error {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	configJSON, err := json.Marshal(v.Config)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("marshal list view config: %w", err))
	}

	query, args, err := r.psql().Insert("sys_list_views").
		Columns("id", "entity_type", "name", "author_id", "visibility", "is_default", "sort_order", "config").
		Values(v.ID, v.EntityType, v.Name, v.AuthorID, v.Visibility, v.IsDefault, v.SortOrder, configJSON).
		Suffix("RETURNING deletion_mark, version, created_at, updated_at").
		ToSql()
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("build insert query: %w", err))
	}

	err = querier.QueryRow(ctx, query, args...).Scan(&v.DeletionMark, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("execute insert: %w", err))
	}
	return nil
}

// Update modifies an existing list view with optimistic locking.
func (r *ListViewRepo) Update(ctx context.Context, v *listview.ListView) error {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	configJSON, err := json.Marshal(v.Config)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("marshal list view config: %w", err))
	}

	query, args, err := r.psql().Update("sys_list_views").
		Set("name", v.Name).
		Set("visibility", v.Visibility).
		Set("is_default", v.IsDefault).
		Set("sort_order", v.SortOrder).
		Set("config", configJSON).
		Set("version", squirrel.Expr("version + 1")).
		Where(squirrel.Eq{"id": v.ID, "deletion_mark": false}).
		Suffix("RETURNING deletion_mark, version, created_at, updated_at").
		ToSql()
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("build update query: %w", err))
	}

	err = querier.QueryRow(ctx, query, args...).Scan(&v.DeletionMark, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewNotFound("sys_list_views", v.ID)
		}
		return apperror.NewInternal(fmt.Errorf("execute update: %w", err))
	}
	return nil
}

// Delete soft-deletes a list view.
func (r *ListViewRepo) Delete(ctx context.Context, id uuid.UUID) error {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	query, args, err := r.psql().Update("sys_list_views").
		Set("deletion_mark", true).
		Set("version", squirrel.Expr("version + 1")).
		Where(squirrel.Eq{"id": id, "deletion_mark": false}).
		ToSql()
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("build delete query: %w", err))
	}

	cmdTag, err := querier.Exec(ctx, query, args...)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("execute delete: %w", err))
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_list_views", id)
	}
	return nil
}

// GetByID returns a single list view by ID.
func (r *ListViewRepo) GetByID(ctx context.Context, id uuid.UUID) (*listview.ListView, error) {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	query, args, err := r.psql().Select(listViewColumns...).
		From("sys_list_views").
		Where(squirrel.Eq{"id": id, "deletion_mark": false}).
		ToSql()
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("build query: %w", err))
	}

	var v listview.ListView
	err = scanListView(querier.QueryRow(ctx, query, args...), &v)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.NewNotFound("sys_list_views", id)
		}
		return nil, apperror.NewInternal(fmt.Errorf("scan list view: %w", err))
	}
	return &v, nil
}

// GetList returns views for an entity type accessible to the user.
func (r *ListViewRepo) GetList(ctx context.Context, entityType string, userID uuid.UUID) ([]*listview.ListView, error) {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	cond := squirrel.And{
		squirrel.Eq{"entity_type": entityType},
		squirrel.Eq{"deletion_mark": false},
		squirrel.Or{
			squirrel.Eq{"visibility": []string{
				string(listview.VisibilitySystem),
				string(listview.VisibilityShared),
			}},
			squirrel.And{
				squirrel.Eq{"visibility": string(listview.VisibilityPersonal)},
				squirrel.Eq{"author_id": userID},
			},
		},
	}

	query, args, err := r.psql().Select(listViewColumns...).
		From("sys_list_views").
		Where(cond).
		OrderBy("sort_order ASC", "name ASC").
		ToSql()
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("build query: %w", err))
	}

	rows, err := querier.Query(ctx, query, args...)
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("execute query: %w", err))
	}
	defer rows.Close()

	var list []*listview.ListView
	for rows.Next() {
		v := &listview.ListView{}
		if err := scanListView(rows, v); err != nil {
			return nil, apperror.NewInternal(fmt.Errorf("scan list view row: %w", err))
		}
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("rows iteration error: %w", err))
	}

	return list, nil
}

// ClearDefault resets is_default=false for all views of an entity type
// belonging to the user (personal scope) or shared/system scope.
func (r *ListViewRepo) ClearDefault(ctx context.Context, entityType string, userID uuid.UUID) error {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	cond := squirrel.And{
		squirrel.Eq{"entity_type": entityType},
		squirrel.Eq{"is_default": true},
		squirrel.Eq{"deletion_mark": false},
		squirrel.Or{
			squirrel.Eq{"visibility": []string{
				string(listview.VisibilitySystem),
				string(listview.VisibilityShared),
			}},
			squirrel.And{
				squirrel.Eq{"visibility": string(listview.VisibilityPersonal)},
				squirrel.Eq{"author_id": userID},
			},
		},
	}

	query, args, err := r.psql().Update("sys_list_views").
		Set("is_default", false).
		Where(cond).
		ToSql()
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("build clear default query: %w", err))
	}

	if _, err := querier.Exec(ctx, query, args...); err != nil {
		return apperror.NewInternal(fmt.Errorf("execute clear default: %w", err))
	}
	return nil
}

// Ensure interface compliance.
var _ listview.Repository = (*ListViewRepo)(nil)
