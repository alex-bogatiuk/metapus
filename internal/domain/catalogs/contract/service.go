package contract

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Service provides business logic for Contract catalog.
// Uses composition with domain.CatalogService for common CRUD operations.
type Service struct {
	*domain.CatalogService[*Contract] // Embedded for delegation
	repo                              Repository
	numerator                         numerator.Generator
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
		EntityName: "contract",
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
		numerator:      numerator,
	}

	base.Hooks().OnBeforeCreate(svc.prepareForCreate)

	return svc
}

// prepareForCreate handles code generation.
func (s *Service) prepareForCreate(ctx context.Context, c *Contract) error {
	// Generate code if not provided
	if c.Code == "" {
		code, err := s.numerator.GetNextNumber(ctx, numerator.DefaultConfig("CT"), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		c.Code = code
	}

	return nil
}

// --- Entity-specific methods ---

// FindByCounterparty retrieves contracts for a counterparty.
func (s *Service) FindByCounterparty(ctx context.Context, counterpartyID id.ID) ([]*Contract, error) {
	return s.repo.FindByCounterparty(ctx, counterpartyID)
}
