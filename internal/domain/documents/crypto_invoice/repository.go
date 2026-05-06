package crypto_invoice

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines operations for crypto invoice documents.
type Repository interface {
	// CRUD operations
	Create(ctx context.Context, doc *CryptoInvoice) error
	GetByID(ctx context.Context, docID id.ID) (*CryptoInvoice, error)
	GetByNumber(ctx context.Context, number string) (*CryptoInvoice, error)
	Update(ctx context.Context, doc *CryptoInvoice) error
	Delete(ctx context.Context, docID id.ID) error

	// Line operations
	GetLines(ctx context.Context, docID id.ID) ([]CryptoInvoiceLine, error)
	SaveLines(ctx context.Context, docID id.ID, lines []CryptoInvoiceLine) error

	// List operations
	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*CryptoInvoice], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)

	// Locking
	GetForUpdate(ctx context.Context, docID id.ID) (*CryptoInvoice, error)

	// Crypto-specific
	FindByExternalID(ctx context.Context, externalID string) (*CryptoInvoice, error)

	// ExpireOverdue marks invoices past their expires_at as Expired.
	// Returns the number of invoices expired. Used by the worker's expiration loop.
	ExpireOverdue(ctx context.Context) (int64, error)
}
