package catalog_repo

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/rate_source"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// RateSourceRepo implements rate_source.Repository.
type RateSourceRepo struct {
	*BaseCatalogRepo[*rate_source.RateSource]
}

// NewRateSourceRepo creates a new rate source repository.
func NewRateSourceRepo() *RateSourceRepo {
	return &RateSourceRepo{
		BaseCatalogRepo: NewCatalogRepoForDefinition[*rate_source.RateSource](rate_source.Definition,
			postgres.ExtractDBColumns[rate_source.RateSource](),
			func() *rate_source.RateSource { return &rate_source.RateSource{} }),
	}
}

func (r *RateSourceRepo) IsUsed(ctx context.Context, sourceID id.ID) (bool, error) {
	var used bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_rate_source_mappings WHERE rate_source_id=$1) OR EXISTS(SELECT 1 FROM reg_exchange_rates WHERE rate_source_id=$1) OR EXISTS(SELECT 1 FROM sys_settings WHERE exchange_rates->>'sourceId'=$1::text)`, sourceID).Scan(&used)
	return used, err
}

func (r *RateSourceRepo) validatePool(ctx context.Context, s *rate_source.RateSource) error {
	if err := LockIntegrationConfiguration(ctx); err != nil {
		return err
	}
	if s.IntegrationPoolID == nil {
		return nil
	}
	if s.SourceType != "coingecko" {
		return apperror.NewValidation("Пул аккаунтов поддерживается источником CoinGecko")
	}
	var valid bool
	if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_integration_pools WHERE id=$1 AND provider='coingecko_demo' AND NOT deletion_mark AND _deleted_at IS NULL)`, s.IntegrationPoolID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return apperror.NewValidation("Выберите пул CoinGecko Demo")
	}
	s.APIKey = nil
	return nil
}
func (r *RateSourceRepo) Create(ctx context.Context, s *rate_source.RateSource) error {
	if err := r.validatePool(ctx, s); err != nil {
		return err
	}
	return r.BaseCatalogRepo.Create(ctx, s)
}
func (r *RateSourceRepo) Update(ctx context.Context, s *rate_source.RateSource) error {
	if err := r.validatePool(ctx, s); err != nil {
		return err
	}
	return r.BaseCatalogRepo.Update(ctx, s)
}
