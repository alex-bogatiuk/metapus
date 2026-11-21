package crypto_sweep

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

// Service provides business operations for crypto sweep documents.
type Service struct {
	*domain.BaseDocumentService[*CryptoSweep, CryptoSweepLine]
	repo Repository
}

// NewService creates a new crypto sweep service.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
) *Service {
	base := domain.NewBaseDocumentService(domain.BaseDocumentServiceConfig[*CryptoSweep, CryptoSweepLine]{
		Repo:          repo,
		PostingEngine: postingEngine,
		Numerator:     num,
		TxManager:     txManager,
		Definition:    &Definition,
	})
	return &Service{BaseDocumentService: base, repo: repo}
}

// Hooks returns the hook registry.
func (s *Service) Hooks() *domain.HookRegistry[*CryptoSweep] {
	return s.GetHooks()
}

// RetryHeld re-queues an unsigned held sweep document for execution.
func (s *Service) RetryHeld(ctx context.Context, docID, actorID id.ID) error {
	if id.IsNil(docID) {
		return apperror.NewValidation("crypto sweep id is required")
	}
	if id.IsNil(actorID) {
		return apperror.NewUnauthorized("authenticated user id is required")
	}
	txm, err := s.GetTxManager(ctx)
	if err != nil {
		return fmt.Errorf("get tx manager: %w", err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		updated, err := s.repo.RetryHeldSweep(txCtx, docID, actorID)
		if err != nil {
			return fmt.Errorf("retry held crypto sweep: %w", err)
		}
		if !updated {
			return apperror.NewConflict("only held unsigned crypto sweep can be retried")
		}
		return nil
	})
}
