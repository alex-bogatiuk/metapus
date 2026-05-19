package counterparty

import (
	"context"

	"metapus/internal/domain"
)

// Repository defines the interface for Counterparty persistence.
type Repository interface {
	domain.CatalogRepository[*Counterparty]

	// FindByINN retrieves counterparty by INN (unique within tenant).
	FindByINN(ctx context.Context, inn string) (*Counterparty, error)

}
