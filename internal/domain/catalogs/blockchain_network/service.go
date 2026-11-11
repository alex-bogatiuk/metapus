package blockchain_network

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
)

// Repository defines storage operations for BlockchainNetwork.
type Repository interface {
	domain.CatalogRepository[*BlockchainNetwork]

	FindByFamilyIdentifier(ctx context.Context, family crypto.ChainFamily, networkIdentifier string) (*BlockchainNetwork, error)
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
		Definition: &Definition,
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
		net.Code = string(net.ChainFamily) + "-" + net.NetworkIdentifier
	}

	if exists, _ := s.checkNetworkIdentityExists(ctx, net.ChainFamily, net.NetworkIdentifier, net.ID); exists {
		return apperror.NewConflict("blockchain network with this family and identifier already exists").
			WithDetail("chainFamily", net.ChainFamily).
			WithDetail("networkIdentifier", net.NetworkIdentifier)
	}
	return nil
}

// prepareForUpdate validates chain ID uniqueness on update. Watching is kept:
// it changes only through the guarded crypto network command.
func (s *Service) prepareForUpdate(ctx context.Context, net *BlockchainNetwork) error {
	current, err := s.repo.GetByID(ctx, net.ID)
	if err != nil {
		return err
	}
	net.IsActive = current.IsActive
	if exists, _ := s.checkNetworkIdentityExists(ctx, net.ChainFamily, net.NetworkIdentifier, net.ID); exists {
		return apperror.NewConflict("blockchain network with this family and identifier already exists").
			WithDetail("chainFamily", net.ChainFamily).
			WithDetail("networkIdentifier", net.NetworkIdentifier)
	}
	return nil
}

func (s *Service) FindByFamilyIdentifier(ctx context.Context, family crypto.ChainFamily, networkIdentifier string) (*BlockchainNetwork, error) {
	return s.repo.FindByFamilyIdentifier(ctx, family, networkIdentifier)
}

func (s *Service) checkNetworkIdentityExists(ctx context.Context, family crypto.ChainFamily, networkIdentifier string, excludeID interface{ String() string }) (bool, error) {
	existing, err := s.repo.FindByFamilyIdentifier(ctx, family, networkIdentifier)
	if err != nil {
		return false, nil
	}
	return existing.ID.String() != excludeID.String(), nil
}
