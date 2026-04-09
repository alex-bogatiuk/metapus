package warehouse

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines the interface for Warehouse persistence.
type Repository interface {
	domain.CatalogRepository[*Warehouse]

	// GetForUpdate retrieves warehouse with row lock (for transactional updates).
	GetForUpdate(ctx context.Context, id id.ID) (*Warehouse, error)

	// ClearDefault clears the default flag on all warehouses (before setting new default).
	ClearDefault(ctx context.Context) error
}
