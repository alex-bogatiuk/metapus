package crypto_network_operation

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

type Repository interface {
	LockResources(ctx context.Context, docID id.ID, extra ...id.ID) error
	Create(ctx context.Context, doc *CryptoNetworkOperation) error
	GetByID(ctx context.Context, docID id.ID) (*CryptoNetworkOperation, error)
	GetByIDForUpdate(ctx context.Context, docID id.ID) (*CryptoNetworkOperation, error)
	GetByNumber(ctx context.Context, number string) (*CryptoNetworkOperation, error)
	Update(ctx context.Context, doc *CryptoNetworkOperation) error
	Delete(ctx context.Context, docID id.ID) error
	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*CryptoNetworkOperation], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)
	SetManualEditAllowed(ctx context.Context, docID id.ID, allowed bool) error
	ValidatePostingReferences(ctx context.Context, doc *CryptoNetworkOperation) error
}
