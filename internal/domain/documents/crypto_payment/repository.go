package crypto_payment

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines operations for crypto payment documents (header-only, no lines).
type Repository interface {
	// CRUD operations
	Create(ctx context.Context, doc *CryptoPayment) error
	GetByID(ctx context.Context, docID id.ID) (*CryptoPayment, error)
	GetByNumber(ctx context.Context, number string) (*CryptoPayment, error)
	Update(ctx context.Context, doc *CryptoPayment) error
	Delete(ctx context.Context, docID id.ID) error

	// List operations
	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*CryptoPayment], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)


	// Crypto-specific
	FindByTxHash(ctx context.Context, txHash string) (*CryptoPayment, error)
	ListByStatus(ctx context.Context, status PaymentStatus) ([]*CryptoPayment, error)
}
