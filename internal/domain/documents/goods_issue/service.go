package goods_issue

import (
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

// Service provides business operations for goods issue documents.
// Embeds BaseDocumentService for common CRUD + posting logic.
type Service struct {
	*domain.BaseDocumentService[*GoodsIssue, GoodsIssueLine]
}

// NewService creates a new goods issue service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	num numerator.Generator,
	txManager tx.Manager,
	currencyStrategy domain.CurrencyResolveStrategy,
) *Service {
	base := domain.NewBaseDocumentService(domain.BaseDocumentServiceConfig[*GoodsIssue, GoodsIssueLine]{
		Repo:              repo,
		PostingEngine:     postingEngine,
		Numerator:         num,
		TxManager:         txManager,
		CurrencyResolver:  currencyStrategy,
		NumeratorPrefix:   "GI",
		NumeratorStrategy: NumeratorStrategy,
		EntityName:        "goods issue",
	})
	return &Service{BaseDocumentService: base}
}

// Hooks returns the hook registry for registering callbacks.
func (s *Service) Hooks() *domain.HookRegistry[*GoodsIssue] {
	return s.BaseDocumentService.GetHooks()
}
