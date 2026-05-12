package crypto_withdrawal

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines operations for crypto withdrawal documents (header-only, no lines).
type Repository interface {
	Create(ctx context.Context, doc *CryptoWithdrawal) error
	GetByID(ctx context.Context, docID id.ID) (*CryptoWithdrawal, error)
	GetByNumber(ctx context.Context, number string) (*CryptoWithdrawal, error)
	Update(ctx context.Context, doc *CryptoWithdrawal) error
	Delete(ctx context.Context, docID id.ID) error

	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*CryptoWithdrawal], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)

	GetForUpdate(ctx context.Context, docID id.ID) (*CryptoWithdrawal, error)

	// FindPending returns withdrawals in Created status (for processing queue).
	FindPending(ctx context.Context, limit int) ([]*CryptoWithdrawal, error)
}
