package nomenclature

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines the interface for Nomenclature persistence.
type Repository interface {
	domain.CatalogRepository[*Nomenclature]

	// FindByArticle retrieves nomenclature by article.
	FindByArticle(ctx context.Context, article string) (*Nomenclature, error)

	// FindByBarcode retrieves nomenclature by barcode.
	FindByBarcode(ctx context.Context, barcode string) (*Nomenclature, error)

	// GetForUpdate retrieves nomenclature with row lock.
	GetForUpdate(ctx context.Context, id id.ID) (*Nomenclature, error)

	// FindLowStock retrieves items with stock below minimum.
	FindLowStock(ctx context.Context, filter domain.ListFilter) (domain.ListResult[*Nomenclature], error)
}
