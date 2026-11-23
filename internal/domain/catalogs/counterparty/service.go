package counterparty

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

// Service provides business logic for Counterparty catalog.
// Uses composition with domain.CatalogService for common CRUD operations.
type Service struct {
	*domain.CatalogService[*Counterparty] // Embedded for delegation
	repo                                  Repository
}

// NewService creates a new Counterparty service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	numerator numerator.Generator,
) *Service {
	// Create base service
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Counterparty]{
		Repo:       repo,
		TxManager:  nil, // Will be obtained from context
		Numerator:  numerator,
		Definition: &Definition,
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
	}

	// Register hooks for entity-specific logic
	base.Hooks().OnBeforeCreate(svc.prepareForCreate)
	base.Hooks().OnBeforeUpdate(svc.prepareForUpdate)

	return svc
}

// prepareForCreate checks INN uniqueness before create; the platform assigns
// an empty code from the series of the Definition.
func (s *Service) prepareForCreate(ctx context.Context, cp *Counterparty) error {
	// Check INN uniqueness
	if cp.INN != nil && *cp.INN != "" {
		exists, err := s.checkINNExists(ctx, *cp.INN, cp.ID)
		if err != nil {
			return err
		}
		if exists {
			return apperror.NewConflict("counterparty with this INN already exists").
				WithDetail("inn", cp.INN)
		}
	}

	return nil
}

// prepareForUpdate handles uniqueness checks before update.
func (s *Service) prepareForUpdate(ctx context.Context, cp *Counterparty) error {
	// Check INN uniqueness (exclude current record)
	if cp.INN != nil && *cp.INN != "" {
		exists, err := s.checkINNExists(ctx, *cp.INN, cp.ID)
		if err != nil {
			return err
		}
		if exists {
			return apperror.NewConflict("counterparty with this INN already exists").
				WithDetail("inn", cp.INN)
		}
	}

	return nil
}

// --- Entity-specific methods (not in base CatalogService) ---

// FindByINN retrieves counterparty by INN.
func (s *Service) FindByINN(ctx context.Context, inn string) (*Counterparty, error) {
	return s.repo.FindByINN(ctx, inn)
}

// checkINNExists checks if INN is already used by another counterparty.
func (s *Service) checkINNExists(ctx context.Context, inn string, excludeID id.ID) (bool, error) {
	existing, err := s.repo.FindByINN(ctx, inn)
	if err != nil {
		// Not found is OK; other errors must be propagated (DB errors, timeouts, etc.).
		if apperror.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	// If found and it's a different record
	return existing.ID != excludeID, nil
}
