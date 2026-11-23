package crypto_provider_balance_operation

import (
	"context"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type Repository interface {
	Create(context.Context, *CryptoProviderBalanceOperation) error
	GetByID(context.Context, id.ID) (*CryptoProviderBalanceOperation, error)
	GetByIDForUpdate(context.Context, id.ID) (*CryptoProviderBalanceOperation, error)
	GetByNumber(context.Context, string) (*CryptoProviderBalanceOperation, error)
	Update(context.Context, *CryptoProviderBalanceOperation) error
	Delete(context.Context, id.ID) error
	List(context.Context, domain.ListFilter) (domain.CursorListResult[*CryptoProviderBalanceOperation], error)
	ListIDs(context.Context, domain.ListFilter, int) ([]id.ID, error)
}
