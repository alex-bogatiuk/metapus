package crypto_payment

import (
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

// Service provides business operations for crypto payment documents.
// Embeds BaseHeaderDocumentService — no line items (header-only).
type Service struct {
	*domain.BaseHeaderDocumentService[*CryptoPayment]
}

// NewService creates a new crypto payment service.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
) *Service {
	base := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*CryptoPayment]{
		Repo:              repo,
		PostingEngine:     postingEngine,
		Numerator:         num,
		TxManager:         txManager,
		NumeratorPrefix:   "CP",
		NumeratorStrategy: _numeratorStrategy,
		EntityName:        "crypto_payment",
	})
	return &Service{BaseHeaderDocumentService: base}
}

// Hooks returns the hook registry for registering callbacks.
func (s *Service) Hooks() *domain.HookRegistry[*CryptoPayment] {
	return s.GetHooks()
}
