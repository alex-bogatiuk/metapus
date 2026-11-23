package catalog_repo

import (
	"context"

	"github.com/Masterminds/squirrel"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/counterparty"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// CounterpartyRepo implements counterparty.Repository.
type CounterpartyRepo struct {
	*BaseCatalogRepo[*counterparty.Counterparty]
}

// NewCounterpartyRepo creates a new counterparty repository.
func NewCounterpartyRepo() *CounterpartyRepo {
	repo := &CounterpartyRepo{
		BaseCatalogRepo: NewCatalogRepoForDefinition[*counterparty.Counterparty](counterparty.Definition,
			postgres.ExtractDBColumns[counterparty.Counterparty](),
			func() *counterparty.Counterparty { return &counterparty.Counterparty{} }),
	}
	repo.SetEntityName(counterparty.Definition.Key)
	return repo
}

// FindByINN retrieves counterparty by INN.
func (r *CounterpartyRepo) FindByINN(ctx context.Context, inn string) (*counterparty.Counterparty, error) {
	q := r.BaseSelect(ctx).
		Where(squirrel.Eq{"inn": inn}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	cp, err := r.FindOne(ctx, q)
	if err != nil {
		if apperror.IsNotFound(err) {
			return nil, apperror.NewNotFound("counterparty", inn)
		}
		return nil, err
	}
	return cp, nil
}
