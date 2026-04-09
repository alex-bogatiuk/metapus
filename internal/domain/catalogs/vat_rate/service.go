package vat_rate

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Service provides business logic for VATRate catalog.
// Uses composition with domain.CatalogService for common CRUD operations.
type Service struct {
	*domain.CatalogService[*VATRate] // Embedded for delegation
	repo                             Repository
	numerator                        numerator.Generator
}

// NewService creates a new VATRate service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	numerator numerator.Generator,
) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*VATRate]{
		Repo:       repo,
		TxManager:  nil, // Will be obtained from context
		Numerator:  numerator,
		EntityName: "vat_rate",
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
func (s *Service) prepareForCreate(ctx context.Context, vr *VATRate) error {
	// Generate code if not provided
	if vr.Code == "" {
		code, err := s.numerator.GetNextNumber(ctx, numerator.DefaultConfig("VR"), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		vr.Code = code
	}

	// Check rate uniqueness (only for non-exempt rates)
	if !vr.IsTaxExempt {
		if exists, _ := s.checkRateExists(ctx, vr.Rate, vr.ID); exists {
			return apperror.NewConflict("VAT rate with this percentage already exists").
				WithDetail("rate", vr.Rate.String())
		}
	}

	return nil
}

// prepareForUpdate handles uniqueness checks.
func (s *Service) prepareForUpdate(ctx context.Context, vr *VATRate) error {
	if !vr.IsTaxExempt {
		if exists, _ := s.checkRateExists(ctx, vr.Rate, vr.ID); exists {
			return apperror.NewConflict("VAT rate with this percentage already exists").
				WithDetail("rate", vr.Rate.String())
		}
	}

	return nil
}

// --- Entity-specific methods ---

// FindByRate retrieves VAT rate by rate value.
func (s *Service) FindByRate(ctx context.Context, rate decimal.Decimal) (*VATRate, error) {
	return s.repo.FindByRate(ctx, rate)
}

func (s *Service) checkRateExists(ctx context.Context, rate decimal.Decimal, excludeID id.ID) (bool, error) {
	existing, err := s.repo.FindByRate(ctx, rate)
	if err != nil {
		return false, nil
	}
	return existing.ID != excludeID, nil
}
