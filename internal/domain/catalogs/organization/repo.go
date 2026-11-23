package organization

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

// Repository defines the interface for organization storage.
type Repository interface {
	domain.CatalogRepository[*Organization]

	GetDefault(ctx context.Context) (*Organization, error)
}
