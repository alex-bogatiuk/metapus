package crypto_network_operation

import (
	"context"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/numerator"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

// Service records network operations for the activation workflow. HTTP only
// reads them; no user command creates, changes, unposts or deletes one.
type Service struct {
	*domain.BaseHeaderDocumentService[*CryptoNetworkOperation]
	repo Repository
}

func NewService(repo Repository, engine *posting.Engine, num numerator.Generator, txm tx.Manager) *Service {
	return &Service{
		BaseHeaderDocumentService: domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*CryptoNetworkOperation]{
			Repo: repo, PostingEngine: engine, Numerator: num, TxManager: txm,
			Definition: &Definition,
		}),
		repo: repo,
	}
}

func (s *Service) Hooks() *domain.HookRegistry[*CryptoNetworkOperation] { return s.GetHooks() }

func (s *Service) Post(ctx context.Context, docID id.ID) error {
	return s.withResources(ctx, docID, func(txCtx context.Context) error {
		doc, err := s.repo.GetByID(txCtx, docID)
		if err != nil {
			return err
		}
		if err := s.repo.ValidatePostingReferences(txCtx, doc); err != nil {
			return err
		}
		return s.BaseHeaderDocumentService.Post(txCtx, docID)
	})
}

func (s *Service) withResources(ctx context.Context, docID id.ID, action func(context.Context) error) error {
	txm, err := s.GetTxManager(ctx)
	if err != nil {
		return err
	}
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.LockResources(ctx, docID); err != nil {
			return err
		}
		return action(ctx)
	})
}
