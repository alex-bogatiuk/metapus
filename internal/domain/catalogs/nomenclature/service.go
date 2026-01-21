package nomenclature

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Service provides business logic for Nomenclature catalog.
// Uses composition with domain.CatalogService for common CRUD operations.
type Service struct {
	*domain.CatalogService[*Nomenclature]
	repo      Repository
	numerator numerator.Generator
}

// NewService creates a new Nomenclature service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	numerator numerator.Generator,
) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Nomenclature]{
		Repo:       repo,
		TxManager:  nil, // Will be obtained from context
		Numerator:  numerator,
		EntityName: "nomenclature",
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
func (s *Service) prepareForCreate(ctx context.Context, item *Nomenclature) error {
	// Generate code if not provided
	if item.Code == "" {
		code, err := s.numerator.GetNextNumber(ctx, numerator.DefaultConfig("NM"), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		item.Code = code
	}

	// Check article uniqueness
	if item.Article != nil && *item.Article != "" {
		if exists, _ := s.checkArticleExists(ctx, *item.Article, item.ID); exists {
			return apperror.NewConflict("item with this article already exists").
				WithDetail("article", item.Article)
		}
	}

	// Check barcode uniqueness
	if item.Barcode != nil && *item.Barcode != "" {
		if exists, _ := s.checkBarcodeExists(ctx, *item.Barcode, item.ID); exists {
			return apperror.NewConflict("item with this barcode already exists").
				WithDetail("barcode", item.Barcode)
		}
	}

	return nil
}

// prepareForUpdate handles uniqueness checks.
func (s *Service) prepareForUpdate(ctx context.Context, item *Nomenclature) error {
	if item.Article != nil && *item.Article != "" {
		if exists, _ := s.checkArticleExists(ctx, *item.Article, item.ID); exists {
			return apperror.NewConflict("item with this article already exists").
				WithDetail("article", item.Article)
		}
	}

	if item.Barcode != nil && *item.Barcode != "" {
		if exists, _ := s.checkBarcodeExists(ctx, *item.Barcode, item.ID); exists {
			return apperror.NewConflict("item with this barcode already exists").
				WithDetail("barcode", item.Barcode)
		}
	}

	return nil
}

// --- Entity-specific methods ---

// FindLowStock retrieves items with stock below minimum.
func (s *Service) FindLowStock(ctx context.Context, filter domain.ListFilter) (domain.ListResult[*Nomenclature], error) {
	return s.repo.FindLowStock(ctx, filter)
}

// FindByArticle retrieves nomenclature by article.
func (s *Service) FindByArticle(ctx context.Context, article string) (*Nomenclature, error) {
	return s.repo.FindByArticle(ctx, article)
}

// FindByBarcode retrieves nomenclature by barcode.
func (s *Service) FindByBarcode(ctx context.Context, barcode string) (*Nomenclature, error) {
	return s.repo.FindByBarcode(ctx, barcode)
}

// checkArticleExists checks if article is already used.
func (s *Service) checkArticleExists(ctx context.Context, article string, excludeID id.ID) (bool, error) {
	existing, err := s.repo.FindByArticle(ctx, article)
	if err != nil {
		return false, nil
	}
	return existing.ID != excludeID, nil
}

// checkBarcodeExists checks if barcode is already used.
func (s *Service) checkBarcodeExists(ctx context.Context, barcode string, excludeID id.ID) (bool, error) {
	existing, err := s.repo.FindByBarcode(ctx, barcode)
	if err != nil {
		return false, nil
	}
	return existing.ID != excludeID, nil
}
