package catalog_repo

import (
	"context"

	"github.com/Masterminds/squirrel"
	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/catalogs/vat_rate"
	"metapus/internal/infrastructure/storage/postgres"
)

const vatRateTable = "cat_vat_rates"

// VATRateRepo implements vat_rate.Repository.
type VATRateRepo struct {
	*BaseCatalogRepo[*vat_rate.VATRate]
}

// NewVATRateRepo creates a new VAT rate repository.
func NewVATRateRepo() *VATRateRepo {
	return &VATRateRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*vat_rate.VATRate](
			vatRateTable,
			postgres.ExtractDBColumns[vat_rate.VATRate](),
			func() *vat_rate.VATRate { return &vat_rate.VATRate{} },
			false, // flat catalog: VAT rates don't support hierarchy
		),
	}
}

// FindByRate retrieves VAT rate by rate value.
func (r *VATRateRepo) FindByRate(ctx context.Context, rate decimal.Decimal) (*vat_rate.VATRate, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"rate": rate}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	vr, err := r.FindOne(ctx, q)
	if err != nil {
		if apperror.IsNotFound(err) {
			return nil, apperror.NewNotFound("vat_rate", rate.String())
		}
		return nil, err
	}
	return vr, nil
}
