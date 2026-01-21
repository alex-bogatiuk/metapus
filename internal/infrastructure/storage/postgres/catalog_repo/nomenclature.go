package catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/nomenclature"
	"metapus/internal/infrastructure/storage/postgres"
)

const nomenclatureTable = "cat_nomenclature"

// NomenclatureRepo implements nomenclature.Repository.
type NomenclatureRepo struct {
	*BaseCatalogRepo[*nomenclature.Nomenclature]
}

// NewNomenclatureRepo creates a new nomenclature repository.
func NewNomenclatureRepo() *NomenclatureRepo {
	return &NomenclatureRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*nomenclature.Nomenclature](
			nomenclatureTable,
			postgres.ExtractDBColumns[nomenclature.Nomenclature](),
		),
	}
}

// FindLowStock retrieves items with stock below minimum.
func (r *NomenclatureRepo) FindLowStock(ctx context.Context, filter domain.ListFilter) (domain.ListResult[*nomenclature.Nomenclature], error) {
	result := domain.ListResult[*nomenclature.Nomenclature]{
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}

	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("name ASC")

	if filter.Limit > 0 {
		q = q.Limit(uint64(filter.Limit))
	}
	if filter.Offset > 0 {
		q = q.Offset(uint64(filter.Offset))
	}

	sql, args, _ := q.ToSql()

	var items []*nomenclature.Nomenclature
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &items, sql, args...); err != nil {
		return result, fmt.Errorf("find low stock: %w", err)
	}
	result.Items = items

	return result, nil
}

// FindByArticle retrieves nomenclature by article.
func (r *NomenclatureRepo) FindByArticle(ctx context.Context, article string) (*nomenclature.Nomenclature, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"article": article}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	item, err := r.FindOne(ctx, q)
	if err != nil {
		if apperror.IsNotFound(err) {
			return nil, apperror.NewNotFound("nomenclature", article)
		}
		return nil, err
	}
	return item, nil
}

// FindByBarcode retrieves nomenclature by barcode.
func (r *NomenclatureRepo) FindByBarcode(ctx context.Context, barcode string) (*nomenclature.Nomenclature, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"barcode": barcode}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	item, err := r.FindOne(ctx, q)
	if err != nil {
		if apperror.IsNotFound(err) {
			return nil, apperror.NewNotFound("nomenclature", barcode)
		}
		return nil, err
	}
	return item, nil
}
