package catalog_repo

import (
	"metapus/internal/domain/catalogs/rate_source"
	"metapus/internal/infrastructure/storage/postgres"
)

const _rateSourceTable = "cat_rate_sources"

// RateSourceRepo implements rate_source.Repository.
type RateSourceRepo struct {
	*BaseCatalogRepo[*rate_source.RateSource]
}

// NewRateSourceRepo creates a new rate source repository.
func NewRateSourceRepo() *RateSourceRepo {
	return &RateSourceRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*rate_source.RateSource](
			_rateSourceTable,
			postgres.ExtractDBColumns[rate_source.RateSource](),
			func() *rate_source.RateSource { return &rate_source.RateSource{} },
			false, // flat catalog
		),
	}
}
