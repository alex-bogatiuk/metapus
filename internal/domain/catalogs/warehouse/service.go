package warehouse

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Service provides business logic for Warehouse catalog.
// Uses composition with domain.CatalogService for common CRUD operations.
type Service struct {
	*domain.CatalogService[*Warehouse] // Embedded for delegation
	repo                               Repository
	numerator                          numerator.Generator
}

// NewService creates a new Warehouse service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	numerator numerator.Generator,
) *Service {
	// Инициализируем базовый Generic сервис
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Warehouse]{
		Repo:       repo,
		TxManager:  nil, // Will be obtained from context
		Numerator:  numerator,
		EntityName: "warehouse",
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

// prepareForCreate handles code generation and default flag.
func (s *Service) prepareForCreate(ctx context.Context, wh *Warehouse) error {
	// Generate code if not provided
	if wh.Code == "" {
		code, err := s.numerator.GetNextNumber(ctx, numerator.DefaultConfig("WH"), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		wh.Code = code
	}

	// If setting as default, clear other defaults
	if wh.IsDefault {
		if err := s.clearDefault(ctx); err != nil {
			return err
		}
	}

	return nil
}

// prepareForUpdate handles default flag.
func (s *Service) prepareForUpdate(ctx context.Context, wh *Warehouse) error {
	if wh.IsDefault {
		if err := s.clearDefault(ctx); err != nil {
			return err
		}
	}

	return nil
}

// --- Entity-specific methods ---

// clearDefault сбрасывает флаг основного склада у всех записей.
func (s *Service) clearDefault(ctx context.Context) error {
	// Это массовая операция обновления, её оставляем в репозитории как есть.
	return s.repo.ClearDefault(ctx)
}
