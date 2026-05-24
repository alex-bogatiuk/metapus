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
	"metapus/internal/domain/filter"
	"metapus/internal/domain/reports/variants"
)

type ReportVariantRepo struct {
}

func NewReportVariantRepo() *ReportVariantRepo {
	return &ReportVariantRepo{}
}

func (r *ReportVariantRepo) psql() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

func (r *ReportVariantRepo) Create(ctx context.Context, v *variants.ReportVariant) error {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	configJSON, err := json.Marshal(v.Config)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("failed to marshal config: %w", err))
	}

	query, args, err := r.psql().Insert("sys_report_variants").
		Columns("id", "dataset_key", "name", "author_id", "visibility", "is_default", "config").
		Values(v.ID, v.DatasetKey, v.Name, v.AuthorID, v.Visibility, v.IsDefault, configJSON).
		Suffix("RETURNING deletion_mark, version, created_at, updated_at").
		ToSql()
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("failed to build insert query: %w", err))
	}

	err = querier.QueryRow(ctx, query, args...).Scan(&v.DeletionMark, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("failed to execute insert: %w", err))
	}
	return nil
}

func (r *ReportVariantRepo) Update(ctx context.Context, v *variants.ReportVariant) error {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	configJSON, err := json.Marshal(v.Config)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("failed to marshal config: %w", err))
	}

	query, args, err := r.psql().Update("sys_report_variants").
		Set("name", v.Name).
		Set("visibility", v.Visibility).
		Set("is_default", v.IsDefault).
		Set("config", configJSON).
		Set("version", squirrel.Expr("version + 1")).
		Where(squirrel.Eq{"id": v.ID, "deletion_mark": false}).
		Suffix("RETURNING deletion_mark, version, created_at, updated_at").
		ToSql()
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("failed to build update query: %w", err))
	}

	err = querier.QueryRow(ctx, query, args...).Scan(&v.DeletionMark, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewNotFound("sys_report_variants", v.ID)
		}
		return apperror.NewInternal(fmt.Errorf("failed to execute update: %w", err))
	}
	return nil
}

func (r *ReportVariantRepo) Delete(ctx context.Context, id uuid.UUID) error {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	// Soft delete
	query, args, err := r.psql().Update("sys_report_variants").
		Set("deletion_mark", true).
		Set("version", squirrel.Expr("version + 1")).
		Where(squirrel.Eq{"id": id, "deletion_mark": false}).
		ToSql()
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("failed to build delete query: %w", err))
	}

	cmdTag, err := querier.Exec(ctx, query, args...)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("failed to execute delete: %w", err))
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_report_variants", id)
	}
	return nil
}

func scanVariant(row pgx.Row, v *variants.ReportVariant) error {
	var configJSON []byte
	err := row.Scan(
		&v.ID, &v.DatasetKey, &v.Name, &v.AuthorID, &v.Visibility, &v.IsDefault, &configJSON,
		&v.DeletionMark, &v.Version, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(configJSON, &v.Config); err != nil {
		return apperror.NewInternal(fmt.Errorf("failed to unmarshal config: %w", err))
	}

	// Initialize empty slices if null
	if v.Config.SelectedFields == nil {
		v.Config.SelectedFields = make([]string, 0)
	}
	if v.Config.VisibleColumns == nil {
		v.Config.VisibleColumns = make([]string, 0)
	}
	if v.Config.GroupBy == nil {
		v.Config.GroupBy = make([]string, 0)
	}
	if v.Config.AdvancedFilters == nil {
		v.Config.AdvancedFilters = make([]filter.Item, 0)
	}
	if v.Config.Filters == nil {
		v.Config.Filters = make(map[string]any)
	}

	return nil
}

func (r *ReportVariantRepo) GetByID(ctx context.Context, id uuid.UUID) (*variants.ReportVariant, error) {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	query, args, err := r.psql().Select("id", "dataset_key", "name", "author_id", "visibility", "is_default", "config", "deletion_mark", "version", "created_at", "updated_at").
		From("sys_report_variants").
		Where(squirrel.Eq{"id": id, "deletion_mark": false}).
		ToSql()
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("failed to build query: %w", err))
	}

	var v variants.ReportVariant
	err = scanVariant(querier.QueryRow(ctx, query, args...), &v)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.NewNotFound("sys_report_variants", id)
		}
		return nil, apperror.NewInternal(fmt.Errorf("failed to scan variant: %w", err))
	}
	return &v, nil
}

func (r *ReportVariantRepo) GetList(ctx context.Context, datasetKey string, userID uuid.UUID) ([]*variants.ReportVariant, error) {
	txManager := MustGetTxManager(ctx)
	querier := txManager.GetQuerier(ctx)

	// Fetch System, Shared, and Personal (only for this user)
	cond := squirrel.And{
		squirrel.Eq{"dataset_key": datasetKey},
		squirrel.Eq{"deletion_mark": false},
		squirrel.Or{
			squirrel.Eq{"visibility": []string{string(variants.VisibilitySystem), string(variants.VisibilityShared)}},
			squirrel.And{
				squirrel.Eq{"visibility": string(variants.VisibilityPersonal)},
				squirrel.Eq{"author_id": userID},
			},
		},
	}

	query, args, err := r.psql().Select("id", "dataset_key", "name", "author_id", "visibility", "is_default", "config", "deletion_mark", "version", "created_at", "updated_at").
		From("sys_report_variants").
		Where(cond).
		OrderBy("name ASC").
		ToSql()
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("failed to build query: %w", err))
	}

	rows, err := querier.Query(ctx, query, args...)
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("failed to execute query: %w", err))
	}
	defer rows.Close()

	var list []*variants.ReportVariant
	for rows.Next() {
		v := &variants.ReportVariant{}
		if err := scanVariant(rows, v); err != nil {
			return nil, apperror.NewInternal(fmt.Errorf("failed to scan variant row: %w", err))
		}
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("rows iteration error: %w", err))
	}

	return list, nil
}
