package vehicle

import (
	"context"

	"metapus/internal/domain"
)

// Repository defines the interface for Vehicle persistence.
type Repository interface {
	domain.CatalogRepository[*Vehicle]

	// FindByPlateNumber retrieves vehicle by plate number (unique within tenant).
	FindByPlateNumber(ctx context.Context, plateNumber string) (*Vehicle, error)
}
