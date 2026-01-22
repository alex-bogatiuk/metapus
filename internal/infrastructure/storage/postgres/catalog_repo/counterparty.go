package catalog_repo

import (
	"context"

	"github.com/Masterminds/squirrel"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/infrastructure/storage/postgres"
)

const counterpartyTable = "cat_counterparties"

// CounterpartyRepo implements counterparty.Repository.
type CounterpartyRepo struct {
	*BaseCatalogRepo[*counterparty.Counterparty]
}

// NewCounterpartyRepo creates a new counterparty repository.
func NewCounterpartyRepo() *CounterpartyRepo {
	return &CounterpartyRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*counterparty.Counterparty](
			counterpartyTable,
			postgres.ExtractDBColumns[counterparty.Counterparty](),
			func() *counterparty.Counterparty { return &counterparty.Counterparty{} },
		),
	}
}

// FindByINN retrieves counterparty by INN.
func (r *CounterpartyRepo) FindByINN(ctx context.Context, inn string) (*counterparty.Counterparty, error) {
	q := r.baseSelect(ctx).
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
