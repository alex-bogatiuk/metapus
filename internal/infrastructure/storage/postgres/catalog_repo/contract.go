package catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/contract"
	"metapus/internal/infrastructure/storage/postgres"
)

const contractTable = "cat_contracts"

// ContractRepo implements contract.Repository.
type ContractRepo struct {
	*BaseCatalogRepo[*contract.Contract]
}

// NewContractRepo creates a new contract repository.
func NewContractRepo() *ContractRepo {
	return &ContractRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*contract.Contract](
			contractTable,
			postgres.ExtractDBColumns[contract.Contract](),
			func() *contract.Contract { return &contract.Contract{} },
			false, // flat catalog: contracts don't support hierarchy
		),
	}
}

// FindByCounterparty retrieves contracts for a counterparty.
func (r *ContractRepo) FindByCounterparty(ctx context.Context, counterpartyID id.ID) ([]*contract.Contract, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"counterparty_id": counterpartyID}).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("name ASC")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var contracts []*contract.Contract
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &contracts, sql, args...); err != nil {
		return nil, fmt.Errorf("find by counterparty: %w", err)
	}

	return contracts, nil
}
