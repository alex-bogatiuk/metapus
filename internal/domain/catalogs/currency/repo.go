package currency

import (
	"context"
	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines the interface for Currency persistence.
type Repository interface {
	domain.CatalogRepository[*Currency]

	// FindByISOCode retrieves currency by ISO code (unique within tenant).
	FindByISOCode(ctx context.Context, isoCode string) (*Currency, error)

	// GetForUpdate retrieves currency with row lock.
	GetForUpdate(ctx context.Context, id id.ID) (*Currency, error)

	// ClearBase clears the base flag on all currencies (before setting new base).
	ClearBase(ctx context.Context) error
}
