package catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/catalogs/unit"
	"metapus/internal/infrastructure/storage/postgres"
)

const unitTable = "cat_units"

// UnitRepo implements unit.Repository.
type UnitRepo struct {
	*BaseCatalogRepo[*unit.Unit]
}

// NewUnitRepo creates a new unit repository.
func NewUnitRepo() *UnitRepo {
	return &UnitRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*unit.Unit](
			unitTable,
			postgres.ExtractDBColumns[unit.Unit](),
		),
	}
}

// FindBySymbol retrieves unit by symbol.
func (r *UnitRepo) FindBySymbol(ctx context.Context, symbol string) (*unit.Unit, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"symbol": symbol}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var u unit.Unit
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &u, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("unit", symbol)
		}
		return nil, fmt.Errorf("find by symbol: %w", err)
	}

	return &u, nil
}
