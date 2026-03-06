// Package goods_receipt provides the GoodsReceipt document service.
package goods_receipt

import (
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

// Service provides business operations for goods receipt documents.
// Embeds BaseDocumentService for common CRUD + posting logic.
type Service struct {
	*domain.BaseDocumentService[*GoodsReceipt, GoodsReceiptLine]
}

// NewService creates a new goods receipt service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
	currencyStrategy domain.CurrencyResolveStrategy,
) *Service {
	base := domain.NewBaseDocumentService(domain.BaseDocumentServiceConfig[*GoodsReceipt, GoodsReceiptLine]{
		Repo:              repo,
		PostingEngine:     postingEngine,
		Numerator:         num,
		TxManager:         txManager,
		CurrencyResolver:  currencyStrategy,
		NumeratorPrefix:   "GR",
		NumeratorStrategy: NumeratorStrategy,
		EntityName:        "goods receipt",
	})
	return &Service{BaseDocumentService: base}
}

// Hooks returns the hook registry for registering callbacks.
func (s *Service) Hooks() *domain.HookRegistry[*GoodsReceipt] {
	return s.BaseDocumentService.GetHooks()
}
