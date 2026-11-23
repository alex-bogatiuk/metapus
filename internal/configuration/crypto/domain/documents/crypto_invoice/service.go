// Package crypto_invoice provides the CryptoInvoice document service.
package crypto_invoice

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/numerator"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

// Service provides business operations for crypto invoice documents.
// Embeds BaseHeaderDocumentService — no line items (header-only).
type Service struct {
	*domain.BaseHeaderDocumentService[*CryptoInvoice]
}

// OnCreatedInTransaction uses the shared document lifecycle.
func (s *Service) OnCreatedInTransaction(hook domain.Hook[*CryptoInvoice]) {
	s.GetHooks().On(domain.TransactionCreate, hook)
}

// NewService creates a new crypto invoice service.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
) *Service {
	base := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*CryptoInvoice]{
		Repo:          repo,
		PostingEngine: postingEngine,
		Numerator:     num,
		TxManager:     txManager,
		Definition:    &Definition,
	})
	return &Service{BaseHeaderDocumentService: base}
}

// Hooks returns the hook registry for registering callbacks.
func (s *Service) Hooks() *domain.HookRegistry[*CryptoInvoice] {
	return s.GetHooks()
}
