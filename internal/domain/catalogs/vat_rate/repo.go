package vat_rate

import (
	"context"

	"github.com/shopspring/decimal"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines the interface for VATRate persistence.
type Repository interface {
	domain.CatalogRepository[*VATRate]

	// FindByRate retrieves VAT rate by rate value.
	FindByRate(ctx context.Context, rate decimal.Decimal) (*VATRate, error)

	// GetForUpdate retrieves VAT rate with row lock.
	GetForUpdate(ctx context.Context, id id.ID) (*VATRate, error)
}
