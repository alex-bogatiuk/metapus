// Package crypto_invoice provides the CryptoInvoice document service.
package crypto_invoice

import (
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

// Service provides business operations for crypto invoice documents.
// Embeds BaseHeaderDocumentService — no line items (header-only).
type Service struct {
	*domain.BaseHeaderDocumentService[*CryptoInvoice]
}

// NewService creates a new crypto invoice service.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
) *Service {
	base := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*CryptoInvoice]{
		Repo:              repo,
		PostingEngine:     postingEngine,
		Numerator:         num,
		TxManager:         txManager,
		NumeratorPrefix:   "CI",
		NumeratorStrategy: _numeratorStrategy,
		EntityName:        "crypto_invoice",
	})
	return &Service{BaseHeaderDocumentService: base}
}

// Hooks returns the hook registry for registering callbacks.
func (s *Service) Hooks() *domain.HookRegistry[*CryptoInvoice] {
	return s.GetHooks()
}
