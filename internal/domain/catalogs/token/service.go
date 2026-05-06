package token

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Repository defines storage operations for Token.
type Repository interface {
	domain.CatalogRepository[*Token]

	// FindBySymbolAndNetwork retrieves a token by symbol within a specific network.
	FindBySymbolAndNetwork(ctx context.Context, symbol string, networkID id.ID) (*Token, error)
}

// Service provides business logic for Token catalog.
type Service struct {
	*domain.CatalogService[*Token]
	repo Repository
}

// NewService creates a new Token service.
func NewService(repo Repository, num numerator.Generator) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Token]{
		Repo:       repo,
		Numerator:  num,
		EntityName: "token",
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
	}

	base.Hooks().OnBeforeCreate(svc.prepareForCreate)
	base.Hooks().OnBeforeUpdate(svc.prepareForUpdate)

	return svc
}

// prepareForCreate validates symbol+network uniqueness.
func (s *Service) prepareForCreate(ctx context.Context, tok *Token) error {
	if tok.Code == "" {
		tok.Code = tok.Symbol
	}

	if exists, _ := s.checkSymbolExists(ctx, tok.Symbol, tok.NetworkID, tok.ID); exists {
		return apperror.NewConflict("token with this symbol already exists on this network").
			WithDetail("symbol", tok.Symbol)
	}
	return nil
}

// prepareForUpdate validates symbol+network uniqueness on update.
func (s *Service) prepareForUpdate(ctx context.Context, tok *Token) error {
	if exists, _ := s.checkSymbolExists(ctx, tok.Symbol, tok.NetworkID, tok.ID); exists {
		return apperror.NewConflict("token with this symbol already exists on this network").
			WithDetail("symbol", tok.Symbol)
	}
	return nil
}

// FindBySymbolAndNetwork retrieves a token by symbol and network.
func (s *Service) FindBySymbolAndNetwork(ctx context.Context, symbol string, networkID id.ID) (*Token, error) {
	return s.repo.FindBySymbolAndNetwork(ctx, symbol, networkID)
}

func (s *Service) checkSymbolExists(ctx context.Context, symbol string, networkID, excludeID id.ID) (bool, error) {
	existing, err := s.repo.FindBySymbolAndNetwork(ctx, symbol, networkID)
	if err != nil {
		return false, nil
	}
	return existing.ID != excludeID, nil
}
