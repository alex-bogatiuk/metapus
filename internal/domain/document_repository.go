package domain

import (
	"context"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// DocumentRepository defines CRUD + line operations for document entities.
// T is the document type (pointer), L is the line type (value).
type DocumentRepository[T any, L any] interface {
	// Create inserts a new document
	Create(ctx context.Context, doc T) error

	// GetByID retrieves document by ID
	GetByID(ctx context.Context, docID id.ID) (T, error)

	// GetByNumber retrieves document by number
	GetByNumber(ctx context.Context, number string) (T, error)

	// Update modifies existing document (with optimistic locking)
	Update(ctx context.Context, doc T) error

	// Delete performs soft removal
	Delete(ctx context.Context, docID id.ID) error

	// GetLines retrieves table part (lines) for a document
	GetLines(ctx context.Context, docID id.ID) ([]L, error)

	// SaveLines replaces all lines for a document (delete + insert)
	SaveLines(ctx context.Context, docID id.ID, lines []L) error

	// List retrieves documents with filtering and pagination
	List(ctx context.Context, filter ListFilter) (ListResult[T], error)

	// GetForUpdate retrieves document with pessimistic lock (SELECT … FOR UPDATE)
	GetForUpdate(ctx context.Context, docID id.ID) (T, error)
}

// LinesAccessor provides generic access to document lines.
// Document models must implement this to be used with BaseDocumentService.
type LinesAccessor[L any] interface {
	GetLines() []L
	SetLines(lines []L)
}

// CurrencyAwareDoc extends entity.ICurrencyAware with mutation and
// cross-reference accessors needed for currency resolution.
type CurrencyAwareDoc interface {
	entity.ICurrencyAware
	SetCurrencyID(currencyID id.ID)
	GetContractID() *id.ID
	GetOrganizationID() id.ID
}

// CurrencyResolveStrategy defines the strategy for resolving document currency.
// The resolution algorithm is swappable per document type or environment.
//
// Built-in implementation: documents.CurrencyResolver (1C-style chain:
// Document → Contract → Organization → System base currency).
type CurrencyResolveStrategy interface {
	ResolveForDocument(ctx context.Context, explicitCurrencyID id.ID, contractID *id.ID, organizationID id.ID) (id.ID, error)
}
