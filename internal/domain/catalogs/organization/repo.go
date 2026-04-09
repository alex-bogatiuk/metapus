package organization

import (
	"context"
	"metapus/internal/domain"
)

// Repository defines the interface for organization storage.
type Repository interface {
	domain.CatalogRepository[*Organization]

	GetDefault(ctx context.Context) (*Organization, error)
}
