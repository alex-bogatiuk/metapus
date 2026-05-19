package warehouse

import (
	"context"

	"metapus/internal/domain"
)

// Repository defines the interface for Warehouse persistence.
type Repository interface {
	domain.CatalogRepository[*Warehouse]


	// ClearDefault clears the default flag on all warehouses (before setting new default).
	ClearDefault(ctx context.Context) error
}
