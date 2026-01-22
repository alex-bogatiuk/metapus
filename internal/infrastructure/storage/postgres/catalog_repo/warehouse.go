package catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"

	"metapus/internal/domain/catalogs/warehouse"
	"metapus/internal/infrastructure/storage/postgres"
)

const warehouseTable = "cat_warehouses"

// WarehouseRepo implements warehouse.Repository.
type WarehouseRepo struct {
	*BaseCatalogRepo[*warehouse.Warehouse]
}

// NewWarehouseRepo creates a new warehouse repository.
func NewWarehouseRepo() *WarehouseRepo {
	return &WarehouseRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*warehouse.Warehouse](
			warehouseTable,
			postgres.ExtractDBColumns[warehouse.Warehouse](),
			func() *warehouse.Warehouse { return &warehouse.Warehouse{} },
		),
	}
}

// ClearDefault clears the default flag on all warehouses.
func (r *WarehouseRepo) ClearDefault(ctx context.Context) error {
	q := r.Builder().
		Update(warehouseTable).
		Set("is_default", false).
		Where(squirrel.Eq{"is_default": true})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	_, err = querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("clear default: %w", err)
	}

	return nil
}
