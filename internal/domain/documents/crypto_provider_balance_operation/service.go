package crypto_provider_balance_operation

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

type Service struct {
	*domain.BaseHeaderDocumentService[*CryptoProviderBalanceOperation]
}

func NewService(repo Repository, engine *posting.Engine, num numerator.Generator, txm tx.Manager) *Service {
	return &Service{BaseHeaderDocumentService: domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*CryptoProviderBalanceOperation]{
		Repo: repo, PostingEngine: engine, Numerator: num, TxManager: txm,
		NumeratorPrefix: "PBO", Definition: &Definition,
	})}
}

func (s *Service) Hooks() *domain.HookRegistry[*CryptoProviderBalanceOperation] { return s.GetHooks() }

// This service is the read facade. Financial mutations belong exclusively to
// the application workflow; generic deletion must never unpost the ledger.
func workflowRequired() error {
	return apperror.NewForbidden("use the audited provider balance workflow")
}
func (*Service) Create(context.Context, *CryptoProviderBalanceOperation) error {
	return workflowRequired()
}
func (*Service) Update(context.Context, *CryptoProviderBalanceOperation) error {
	return workflowRequired()
}
func (*Service) Delete(context.Context, id.ID) error                { return workflowRequired() }
func (*Service) SetDeletionMark(context.Context, id.ID, bool) error { return workflowRequired() }
func (*Service) Post(context.Context, id.ID) error                  { return workflowRequired() }
func (*Service) Unpost(context.Context, id.ID) error                { return workflowRequired() }
func (*Service) PostAndSave(context.Context, *CryptoProviderBalanceOperation) error {
	return workflowRequired()
}
func (*Service) UpdateAndRepost(context.Context, *CryptoProviderBalanceOperation) error {
	return workflowRequired()
}
