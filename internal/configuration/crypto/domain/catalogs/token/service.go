package token

import (
	"context"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/numerator"
)

// Repository defines storage operations for Token.
type Repository interface {
	domain.CatalogRepository[*Token]

	// FindBySymbolAndNetwork retrieves a token by symbol within a specific network.
	FindBySymbolAndNetwork(ctx context.Context, symbol string, networkID id.ID) (*Token, error)
	// IdentityForUpdate locks the asset row until the catalog update commits.
	IdentityForUpdate(ctx context.Context, tokenID id.ID) (Identity, bool, error)
}

// Identity contains fields that change the meaning of already recorded raw amounts.
type Identity struct {
	NetworkID       id.ID
	ContractAddress string
	DecimalPlaces   int
	Standard        TokenStandard
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
	identity, locked, err := s.repo.IdentityForUpdate(ctx, tok.ID)
	if err != nil {
		return err
	}
	if locked && (identity.NetworkID != tok.NetworkID ||
		identity.ContractAddress != tok.ContractAddress ||
		identity.DecimalPlaces != tok.DecimalPlaces ||
		identity.Standard != tok.Standard) {
		return apperror.NewConflict("token identity cannot change after financial use")
	}
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
