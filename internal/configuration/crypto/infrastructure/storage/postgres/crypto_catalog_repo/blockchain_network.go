package crypto_catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/configuration/crypto/domain/catalogs/blockchain_network"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_integration_repo"
	"metapus/internal/core/apperror"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

// BlockchainNetworkRepo implements blockchain_network.Repository.
type BlockchainNetworkRepo struct {
	*catalog_repo.BaseCatalogRepo[*blockchain_network.BlockchainNetwork]
}

// NewBlockchainNetworkRepo creates a new blockchain network repository.
func NewBlockchainNetworkRepo() *BlockchainNetworkRepo {
	return &BlockchainNetworkRepo{
		BaseCatalogRepo: catalog_repo.NewCatalogRepoForDefinition[*blockchain_network.BlockchainNetwork](blockchain_network.Definition,
			postgres.ExtractDBColumns[blockchain_network.BlockchainNetwork](),
			func() *blockchain_network.BlockchainNetwork { return &blockchain_network.BlockchainNetwork{} }),
	}
}

// Switching a network to database mode shares the configuration lock with RPC
// mutations and validates the complete target topology before committing.
func (r *BlockchainNetworkRepo) Update(ctx context.Context, network *blockchain_network.BlockchainNetwork) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := catalog_repo.LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		return crypto_integration_repo.ValidateRPCNetworkMutation(ctx, network.ID, func(ctx context.Context) error {
			if err := r.BaseCatalogRepo.Update(ctx, network); err != nil {
				return err
			}
			return crypto_integration_repo.NewRPCConfigurationRepo("").ValidateActiveBindings(ctx, network.ID)
		})
	})
}

func (r *BlockchainNetworkRepo) FindByFamilyIdentifier(ctx context.Context, family crypto.ChainFamily, networkIdentifier string) (*blockchain_network.BlockchainNetwork, error) {
	q := r.BaseSelect(ctx).
		Where(squirrel.Eq{"chain_family": family, "network_identifier": networkIdentifier}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var n blockchain_network.BlockchainNetwork
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &n, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("blockchain_network", string(family)+":"+networkIdentifier)
		}
		return nil, fmt.Errorf("find by family and network identifier: %w", err)
	}

	return &n, nil
}
