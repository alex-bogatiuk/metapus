package crypto_sweep

import (
	"context"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// Repository defines operations for crypto sweep documents.
type Repository interface {
	Create(ctx context.Context, doc *CryptoSweep) error
	GetByID(ctx context.Context, docID id.ID) (*CryptoSweep, error)
	GetByIDForUpdate(ctx context.Context, docID id.ID) (*CryptoSweep, error)
	GetByNumber(ctx context.Context, number string) (*CryptoSweep, error)
	Update(ctx context.Context, doc *CryptoSweep) error
	Delete(ctx context.Context, docID id.ID) error

	GetLines(ctx context.Context, docID id.ID) ([]CryptoSweepLine, error)
	SaveLines(ctx context.Context, docID id.ID, lines []CryptoSweepLine) error

	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*CryptoSweep], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)

	// ListByStatus returns sweeps with the specified lifecycle status.
	ListByStatus(ctx context.Context, status SweepStatus, limit int) ([]*CryptoSweep, error)

	// RetryHeldSweep re-queues an unsigned held sweep document and its attempts for execution.
	RetryHeldSweep(ctx context.Context, docID, actorID id.ID) (bool, error)
}
