package unit

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/pkg/numerator"
)

// Service provides business logic for Unit catalog.
// Uses composition with domain.CatalogService for common CRUD operations.
type Service struct {
	*domain.CatalogService[*Unit]
	repo      Repository
	numerator *numerator.Service
}

// NewService creates a new Unit service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	numerator *numerator.Service,
) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Unit]{
		Repo:       repo,
		TxManager:  nil, // Will be obtained from context
		Numerator:  numerator,
		EntityName: "unit",
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
		numerator:      numerator,
	}

	base.Hooks().OnBeforeCreate(svc.prepareForCreate)
	base.Hooks().OnBeforeUpdate(svc.prepareForUpdate)

	return svc
}

// prepareForCreate handles code generation and uniqueness checks.
func (s *Service) prepareForCreate(ctx context.Context, unit *Unit) error {
	// Generate code if not provided
	if unit.Code == "" {
		code, err := s.numerator.GetNextNumber(ctx, numerator.DefaultConfig("UN"), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		unit.Code = code
	}

	// Check symbol uniqueness
	if unit.Symbol != "" {
		if exists, _ := s.checkSymbolExists(ctx, unit.Symbol, unit.ID); exists {
			return apperror.NewConflict("unit with this symbol already exists").
				WithDetail("symbol", unit.Symbol)
		}
	}

	return nil
}

// prepareForUpdate handles uniqueness checks.
func (s *Service) prepareForUpdate(ctx context.Context, unit *Unit) error {
	if unit.Symbol != "" {
		if exists, _ := s.checkSymbolExists(ctx, unit.Symbol, unit.ID); exists {
			return apperror.NewConflict("unit with this symbol already exists").
				WithDetail("symbol", unit.Symbol)
		}
	}

	return nil
}

// --- Entity-specific methods ---

// FindBySymbol retrieves unit by symbol.
func (s *Service) FindBySymbol(ctx context.Context, symbol string) (*Unit, error) {
	return s.repo.FindBySymbol(ctx, symbol)
}

func (s *Service) checkSymbolExists(ctx context.Context, symbol string, excludeID id.ID) (bool, error) {
	existing, err := s.repo.FindBySymbol(ctx, symbol)
	if err != nil {
		return false, nil
	}
	return existing.ID != excludeID, nil
}
