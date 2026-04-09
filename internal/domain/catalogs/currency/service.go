package currency

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Service provides business logic for Currency catalog.
// Uses composition with domain.CatalogService for common CRUD operations.
type Service struct {
	*domain.CatalogService[*Currency]
	repo      Repository
	numerator numerator.Generator
}

// NewService creates a new Currency service.
// In Database-per-Tenant, TxManager is obtained from context, so it's optional here.
func NewService(
	repo Repository,
	numerator numerator.Generator,
) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Currency]{
		Repo:       repo,
		TxManager:  nil, // Will be obtained from context
		Numerator:  numerator,
		EntityName: "currency",
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
		numerator:      numerator,
	}

	base.Hooks().OnBeforeCreate(svc.prepareForCreate)
	base.Hooks().OnBeforeUpdate(svc.prepareForUpdate)
	base.Hooks().OnBeforeDelete(svc.validateBeforeDelete)

	return svc
}

// prepareForCreate handles code generation and uniqueness checks.
func (s *Service) prepareForCreate(ctx context.Context, curr *Currency) error {
	// Use ISO code as code if not provided
	if curr.Code == "" && curr.ISOCode != nil {
		curr.Code = *curr.ISOCode
	}

	// Check ISO code uniqueness
	if exists, _ := s.checkISOCodeExists(ctx, curr.ISOCode, curr.ID); exists {
		return apperror.NewConflict("currency with this ISO code already exists").
			WithDetail("isoCode", curr.ISOCode)
	}

	// If setting as base, clear other base currencies
	if curr.IsBase {
		if err := s.clearBase(ctx); err != nil {
			return err
		}
	}

	return nil
}

// prepareForUpdate handles uniqueness checks.
func (s *Service) prepareForUpdate(ctx context.Context, curr *Currency) error {
	if exists, _ := s.checkISOCodeExists(ctx, curr.ISOCode, curr.ID); exists {
		return apperror.NewConflict("currency with this ISO code already exists").
			WithDetail("isoCode", curr.ISOCode)
	}

	if curr.IsBase {
		if err := s.clearBase(ctx); err != nil {
			return err
		}
	}

	return nil
}

// validateBeforeDelete prevents deletion of base currency.
func (s *Service) validateBeforeDelete(ctx context.Context, curr *Currency) error {
	if curr.IsBase {
		return apperror.NewValidation("cannot delete base currency")
	}
	return nil
}

// --- Entity-specific methods ---

// FindByISOCode retrieves currency by ISO code.
func (s *Service) FindByISOCode(ctx context.Context, isoCode string) (*Currency, error) {
	return s.repo.FindByISOCode(ctx, isoCode)
}

func (s *Service) checkISOCodeExists(ctx context.Context, isoCode *string, excludeID id.ID) (bool, error) {
	if isoCode == nil || *isoCode == "" {
		return false, nil
	}
	existing, err := s.repo.FindByISOCode(ctx, *isoCode)
	if err != nil {
		return false, nil
	}
	return existing.ID != excludeID, nil
}

func (s *Service) clearBase(ctx context.Context) error {
	// Clear is_base on other currencies
	return s.repo.ClearBase(ctx)
}
