package treasury_withdrawal

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines operations for treasury withdrawal documents.
type Repository interface {
	Create(ctx context.Context, doc *TreasuryWithdrawal) error
	GetByID(ctx context.Context, docID id.ID) (*TreasuryWithdrawal, error)
	GetByIDForUpdate(ctx context.Context, docID id.ID) (*TreasuryWithdrawal, error)
	GetByNumber(ctx context.Context, number string) (*TreasuryWithdrawal, error)
	Update(ctx context.Context, doc *TreasuryWithdrawal) error
	Delete(ctx context.Context, docID id.ID) error

	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*TreasuryWithdrawal], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)
	ListByStatus(ctx context.Context, status Status, limit int) ([]*TreasuryWithdrawal, error)
}
