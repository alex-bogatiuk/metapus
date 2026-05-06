package catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/catalogs/blockchain_network"
	"metapus/internal/infrastructure/storage/postgres"
)

const _blockchainNetworkTable = "cat_blockchain_networks"

// BlockchainNetworkRepo implements blockchain_network.Repository.
type BlockchainNetworkRepo struct {
	*BaseCatalogRepo[*blockchain_network.BlockchainNetwork]
}

// NewBlockchainNetworkRepo creates a new blockchain network repository.
func NewBlockchainNetworkRepo() *BlockchainNetworkRepo {
	return &BlockchainNetworkRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*blockchain_network.BlockchainNetwork](
			_blockchainNetworkTable,
			postgres.ExtractDBColumns[blockchain_network.BlockchainNetwork](),
			func() *blockchain_network.BlockchainNetwork { return &blockchain_network.BlockchainNetwork{} },
			false, // flat catalog
		),
	}
}

// FindByChainID retrieves a network by its chain identifier.
func (r *BlockchainNetworkRepo) FindByChainID(ctx context.Context, chainID string) (*blockchain_network.BlockchainNetwork, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"chain_id": chainID}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var n blockchain_network.BlockchainNetwork
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &n, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("blockchain_network", chainID)
		}
		return nil, fmt.Errorf("find by chain id: %w", err)
	}

	return &n, nil
}
