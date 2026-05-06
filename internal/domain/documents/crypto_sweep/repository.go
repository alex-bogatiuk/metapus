package crypto_sweep

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines operations for crypto sweep documents.
type Repository interface {
	Create(ctx context.Context, doc *CryptoSweep) error
	GetByID(ctx context.Context, docID id.ID) (*CryptoSweep, error)
	GetByNumber(ctx context.Context, number string) (*CryptoSweep, error)
	Update(ctx context.Context, doc *CryptoSweep) error
	Delete(ctx context.Context, docID id.ID) error

	GetLines(ctx context.Context, docID id.ID) ([]CryptoSweepLine, error)
	SaveLines(ctx context.Context, docID id.ID, lines []CryptoSweepLine) error

	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*CryptoSweep], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)

	GetForUpdate(ctx context.Context, docID id.ID) (*CryptoSweep, error)
}
