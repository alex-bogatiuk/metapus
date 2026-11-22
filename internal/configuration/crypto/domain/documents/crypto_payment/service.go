package crypto_payment

import (
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/numerator"
	"metapus/pkg/sdk/posting"
	"metapus/pkg/sdk/tx"
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
		Repo:          repo,
		PostingEngine: postingEngine,
		Numerator:     num,
		TxManager:     txManager,
		Definition:    &Definition,
	})
	return &Service{BaseHeaderDocumentService: base}
}

// Hooks returns the hook registry for registering callbacks.
func (s *Service) Hooks() *domain.HookRegistry[*CryptoPayment] {
	return s.GetHooks()
}
