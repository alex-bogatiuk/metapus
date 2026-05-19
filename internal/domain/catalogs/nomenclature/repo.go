package nomenclature

import (
	"context"

	"metapus/internal/domain"
)

// Repository defines the interface for Nomenclature persistence.
type Repository interface {
	domain.CatalogRepository[*Nomenclature]

	// FindByArticle retrieves nomenclature by article.
	FindByArticle(ctx context.Context, article string) (*Nomenclature, error)

	// FindByBarcode retrieves nomenclature by barcode.
	FindByBarcode(ctx context.Context, barcode string) (*Nomenclature, error)


	// FindLowStock retrieves items with stock below minimum.
	FindLowStock(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*Nomenclature], error)
}
