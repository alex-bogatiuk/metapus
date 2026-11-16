package organization

import (
	"context"

	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Service provides business logic for Organization catalog.
type Service struct {
	*domain.CatalogService[*Organization]
	repo      Repository
	numerator numerator.Generator
}

// NewService creates a new Organization service.
func NewService(repo Repository, numerator numerator.Generator) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Organization]{
		Repo:       repo,
		Numerator:  numerator,
		Definition: &Definition,
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
		numerator:      numerator,
	}
	base.Hooks().OnBeforeCreate(func(_ context.Context, org *Organization) error { org.normalize(); return nil })
	base.Hooks().OnBeforeUpdate(func(_ context.Context, org *Organization) error { org.normalize(); return nil })

	return svc
}

// GetDefault retrieves the default organization.
func (s *Service) GetDefault(ctx context.Context) (*Organization, error) {
	return s.repo.GetDefault(ctx)
}
