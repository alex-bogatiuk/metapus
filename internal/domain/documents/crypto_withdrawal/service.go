package crypto_withdrawal

import (
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

// Service provides business operations for crypto withdrawal documents.
type Service struct {
	*domain.BaseDocumentService[*CryptoWithdrawal, CryptoWithdrawalLine]
}

// NewService creates a new crypto withdrawal service.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
) *Service {
	base := domain.NewBaseDocumentService(domain.BaseDocumentServiceConfig[*CryptoWithdrawal, CryptoWithdrawalLine]{
		Repo:              repo,
		PostingEngine:     postingEngine,
		Numerator:         num,
		TxManager:         txManager,
		NumeratorPrefix:   "CW",
		NumeratorStrategy: _numeratorStrategy,
		EntityName:        "crypto_withdrawal",
	})
	return &Service{BaseDocumentService: base}
}

// Hooks returns the hook registry.
func (s *Service) Hooks() *domain.HookRegistry[*CryptoWithdrawal] {
	return s.GetHooks()
}
