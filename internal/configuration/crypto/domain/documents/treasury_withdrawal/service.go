package treasury_withdrawal

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/numerator"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

// Service provides business operations for treasury withdrawal documents.
type Service struct {
	*domain.BaseHeaderDocumentService[*TreasuryWithdrawal]
}

// NewService creates a treasury withdrawal service.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
) *Service {
	base := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*TreasuryWithdrawal]{
		Repo:          repo,
		PostingEngine: postingEngine,
		Numerator:     num,
		TxManager:     txManager,
		Definition:    &Definition,
	})
	return &Service{BaseHeaderDocumentService: base}
}

// Hooks returns the hook registry.
func (s *Service) Hooks() *domain.HookRegistry[*TreasuryWithdrawal] {
	return s.GetHooks()
}
