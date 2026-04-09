package contract

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines the interface for Contract persistence.
type Repository interface {
	domain.CatalogRepository[*Contract]

	// FindByCounterparty retrieves contracts for a counterparty.
	FindByCounterparty(ctx context.Context, counterpartyID id.ID) ([]*Contract, error)

	// GetForUpdate retrieves contract with row lock.
	GetForUpdate(ctx context.Context, id id.ID) (*Contract, error)
}
