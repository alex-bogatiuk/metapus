package blockchain_network

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Repository defines storage operations for BlockchainNetwork.
type Repository interface {
	domain.CatalogRepository[*BlockchainNetwork]

	// FindByChainID retrieves a network by its chain identifier.
	FindByChainID(ctx context.Context, chainID string) (*BlockchainNetwork, error)
}

// Service provides business logic for BlockchainNetwork catalog.
type Service struct {
	*domain.CatalogService[*BlockchainNetwork]
	repo Repository
}

// NewService creates a new BlockchainNetwork service.
func NewService(repo Repository, num numerator.Generator) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*BlockchainNetwork]{
		Repo:       repo,
		Numerator:  num,
		EntityName: "blockchain_network",
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
	}

	base.Hooks().OnBeforeCreate(svc.prepareForCreate)
	base.Hooks().OnBeforeUpdate(svc.prepareForUpdate)

	return svc
}

// prepareForCreate validates chain ID uniqueness.
func (s *Service) prepareForCreate(ctx context.Context, net *BlockchainNetwork) error {
	if net.Code == "" {
		net.Code = net.ChainID
	}

	if exists, _ := s.checkChainIDExists(ctx, net.ChainID, net.ID); exists {
		return apperror.NewConflict("blockchain network with this chain ID already exists").
			WithDetail("chainId", net.ChainID)
	}
	return nil
}

// prepareForUpdate validates chain ID uniqueness on update.
func (s *Service) prepareForUpdate(ctx context.Context, net *BlockchainNetwork) error {
	if exists, _ := s.checkChainIDExists(ctx, net.ChainID, net.ID); exists {
		return apperror.NewConflict("blockchain network with this chain ID already exists").
			WithDetail("chainId", net.ChainID)
	}
	return nil
}

// FindByChainID retrieves a network by chain ID.
func (s *Service) FindByChainID(ctx context.Context, chainID string) (*BlockchainNetwork, error) {
	return s.repo.FindByChainID(ctx, chainID)
}

func (s *Service) checkChainIDExists(ctx context.Context, chainID string, excludeID interface{ String() string }) (bool, error) {
	existing, err := s.repo.FindByChainID(ctx, chainID)
	if err != nil {
		return false, nil
	}
	return existing.ID.String() != excludeID.String(), nil
}
