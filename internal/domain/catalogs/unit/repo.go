package unit

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines the interface for Unit persistence.
type Repository interface {
	domain.CatalogRepository[*Unit]

	// FindBySymbol retrieves unit by symbol (unique within tenant).
	FindBySymbol(ctx context.Context, symbol string) (*Unit, error)

	// GetForUpdate retrieves unit with row lock.
	GetForUpdate(ctx context.Context, id id.ID) (*Unit, error)
}
