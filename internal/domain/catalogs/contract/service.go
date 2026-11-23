package contract

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

// Service provides business logic for Contract catalog.
// Uses composition with domain.CatalogService for common CRUD operations.
type Service struct {
	*domain.CatalogService[*Contract] // Embedded for delegation
	repo                              Repository
}

// NewService creates a new Contract service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	numerator numerator.Generator,
) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Contract]{
		Repo:       repo,
		TxManager:  nil, // Will be obtained from context
		Numerator:  numerator,
		Definition: &Definition,
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
	}

	return svc
}

// --- Entity-specific methods ---

// FindByCounterparty retrieves contracts for a counterparty.
func (s *Service) FindByCounterparty(ctx context.Context, counterpartyID id.ID) ([]*Contract, error) {
	return s.repo.FindByCounterparty(ctx, counterpartyID)
}
