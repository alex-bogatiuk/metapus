package contract

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

// Repository defines the interface for Contract persistence.
type Repository interface {
	domain.CatalogRepository[*Contract]

	// FindByCounterparty retrieves contracts for a counterparty.
	FindByCounterparty(ctx context.Context, counterpartyID id.ID) ([]*Contract, error)
}
