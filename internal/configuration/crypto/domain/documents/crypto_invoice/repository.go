package crypto_invoice

import (
	"context"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// Repository defines operations for crypto invoice documents (header-only, no lines).
type Repository interface {
	// CRUD operations
	Create(ctx context.Context, doc *CryptoInvoice) error
	GetByID(ctx context.Context, docID id.ID) (*CryptoInvoice, error)
	GetByIDForUpdate(ctx context.Context, docID id.ID) (*CryptoInvoice, error)
	GetByNumber(ctx context.Context, number string) (*CryptoInvoice, error)
	Update(ctx context.Context, doc *CryptoInvoice) error
	Delete(ctx context.Context, docID id.ID) error

	// List operations
	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*CryptoInvoice], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)

	// Crypto-specific
	FindByExternalID(ctx context.Context, externalID string) (*CryptoInvoice, error)
	FindByPublicRef(ctx context.Context, publicRef string) (*CryptoInvoice, error)
	FindByMerchantOrderID(ctx context.Context, merchantID id.ID, orderID string) (*CryptoInvoice, error)
	FindByMerchantExternalID(ctx context.Context, merchantID id.ID, externalID string) (*CryptoInvoice, error)

	// Expiration candidates are selected separately; lifecycle transitions stay
	// in InvoiceLifecycleService and never happen through direct repository SQL.
	FindOverdueIDs(ctx context.Context, limit int) ([]id.ID, error)
	LockWalletForInvoice(ctx context.Context, invoiceID id.ID) error
	ReleaseInvoiceWallet(ctx context.Context, invoiceID id.ID, closeReason string) error
	CleanupOrphanedExpiredLeases(ctx context.Context) error
}
