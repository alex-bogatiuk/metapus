package crypto_sweep

import (
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

// Service provides business operations for crypto sweep documents.
type Service struct {
	*domain.BaseDocumentService[*CryptoSweep, CryptoSweepLine]
}

// NewService creates a new crypto sweep service.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
) *Service {
	base := domain.NewBaseDocumentService(domain.BaseDocumentServiceConfig[*CryptoSweep, CryptoSweepLine]{
		Repo:              repo,
		PostingEngine:     postingEngine,
		Numerator:         num,
		TxManager:         txManager,
		NumeratorPrefix:   "SW",
		NumeratorStrategy: _numeratorStrategy,
		EntityName:        "crypto_sweep",
	})
	return &Service{BaseDocumentService: base}
}

// Hooks returns the hook registry.
func (s *Service) Hooks() *domain.HookRegistry[*CryptoSweep] {
	return s.GetHooks()
}
