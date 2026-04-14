// Package v1 — document_registrations.go contains concrete DocumentRegistration
// implementations for all built-in document types.
// This is the "business content" layer — specific documents shipped with Metapus.
// Core interfaces (DocumentRegistration, DocumentDeps) live in document_factory.go.
package v1

import (
	"context"

	"metapus/internal/domain"
	"metapus/internal/domain/audit"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/metadata"
)

// ---------------------------------------------------------------------------
// Concrete factories (business content)
// ---------------------------------------------------------------------------

// GoodsReceiptRegistration wires the GoodsReceipt document type.
type GoodsReceiptRegistration struct{}

func (r *GoodsReceiptRegistration) RoutePrefix() string { return "goods-receipt" }
func (r *GoodsReceiptRegistration) Permission() string  { return "document:goods_receipt" }
func (r *GoodsReceiptRegistration) EntityName() string  { return "GoodsReceipt" }
func (r *GoodsReceiptRegistration) EntityLabel() string {
	return "Поступление товаров"
}
func (r *GoodsReceiptRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Поступление товаров",
		Plural:   "Поступления товаров",
		NewLabel: "Новое поступление",
		Genitive: "поступления товаров",
	}
}
func (r *GoodsReceiptRegistration) EntityStruct() interface{} { return goods_receipt.GoodsReceipt{} }

func (r *GoodsReceiptRegistration) Build(deps DocumentDeps) DocumentRouteHandler {
	repo := document_repo.NewGoodsReceiptRepo()
	service := goods_receipt.NewService(repo, deps.PostingEngine, deps.Numerator, nil, deps.CurrencyResolver)
	service.SetPolicyEngine(deps.PolicyEngine)

	// Audit hooks — identical for all document types
	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *goods_receipt.GoodsReceipt) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *goods_receipt.GoodsReceipt) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	// Decorators: logging + event log
	decorated := domain.Chain[*goods_receipt.GoodsReceipt](
		domain.WithLogging[*goods_receipt.GoodsReceipt]("goods-receipt"),
		domain.WithEventLog[*goods_receipt.GoodsReceipt]("goods_receipt", deps.EventWriter),
	)(service)

	return handlers.NewGoodsReceiptHandler(deps.BaseHandler, decorated, deps.PrintRegistry, deps.PrintRenderer, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// GoodsIssueRegistration wires the GoodsIssue document type.
type GoodsIssueRegistration struct{}

func (r *GoodsIssueRegistration) RoutePrefix() string { return "goods-issue" }
func (r *GoodsIssueRegistration) Permission() string  { return "document:goods_issue" }
func (r *GoodsIssueRegistration) EntityName() string  { return "GoodsIssue" }
func (r *GoodsIssueRegistration) EntityLabel() string { return "Реализация товаров" }
func (r *GoodsIssueRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Реализация товаров",
		Plural:   "Реализации товаров",
		NewLabel: "Новая реализация",
		Genitive: "реализации товаров",
	}
}
func (r *GoodsIssueRegistration) EntityStruct() interface{} { return goods_issue.GoodsIssue{} }

func (r *GoodsIssueRegistration) Build(deps DocumentDeps) DocumentRouteHandler {
	repo := document_repo.NewGoodsIssueRepo()
	service := goods_issue.NewService(repo, deps.PostingEngine, deps.Numerator, nil, deps.CurrencyResolver)
	service.SetPolicyEngine(deps.PolicyEngine)

	// Audit hooks — identical for all document types
	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *goods_issue.GoodsIssue) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *goods_issue.GoodsIssue) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	// Decorators: logging + event log
	decorated := domain.Chain[*goods_issue.GoodsIssue](
		domain.WithLogging[*goods_issue.GoodsIssue]("goods-issue"),
		domain.WithEventLog[*goods_issue.GoodsIssue]("goods_issue", deps.EventWriter),
	)(service)

	return handlers.NewGoodsIssueHandler(deps.BaseHandler, decorated, deps.PrintRegistry, deps.PrintRenderer, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}
