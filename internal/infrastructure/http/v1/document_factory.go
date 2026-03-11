// Package v1 provides HTTP API version 1.
package v1

import (
	"context"

	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/domain/audit"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

// DocumentDeps holds shared dependencies injected into every document factory.
type DocumentDeps struct {
	BaseHandler      *handlers.BaseHandler
	PostingEngine    *posting.Engine
	Numerator        numerator.Generator
	CurrencyResolver domain.CurrencyResolveStrategy
	PolicyEngine     *security.PolicyEngine
}

// DocumentRegistration is the Abstract Factory interface for document types.
// Each document type provides an implementation that wires repo → service → handler.
//
// Adding a new document type:
//  1. Create model, repo, service (embed BaseDocumentService).
//  2. Create handler (embed BaseDocumentHandler).
//  3. Implement DocumentRegistration.
//  4. Append to documentFactories slice below.
type DocumentRegistration interface {
	// RoutePrefix returns the URL path segment, e.g. "goods-receipt".
	RoutePrefix() string
	// Permission returns the permission prefix, e.g. "document:goods_receipt".
	Permission() string
	// EntityName returns the metadata registry name, e.g. "GoodsReceipt".
	EntityName() string
	// EntityLabel returns the human-readable entity label, e.g. "Поступление товаров".
	EntityLabel() string
	// EntityStruct returns a zero-value instance for metadata.Inspect().
	EntityStruct() interface{}
	// Build creates repo, service (with audit hooks), and handler.
	Build(deps DocumentDeps) DocumentRouteHandler
}

// documentFactories is the registry of all document types.
// To add a new document, append its factory here.
var documentFactories = []DocumentRegistration{
	&GoodsReceiptRegistration{},
	&GoodsIssueRegistration{},
}

// ---------------------------------------------------------------------------
// Concrete factories
// ---------------------------------------------------------------------------

// GoodsReceiptRegistration wires the GoodsReceipt document type.
type GoodsReceiptRegistration struct{}

func (r *GoodsReceiptRegistration) RoutePrefix() string { return "goods-receipt" }
func (r *GoodsReceiptRegistration) Permission() string  { return "document:goods_receipt" }
func (r *GoodsReceiptRegistration) EntityName() string  { return "GoodsReceipt" }
func (r *GoodsReceiptRegistration) EntityLabel() string {
	return "Поступление товаров"
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

	// Decorator: wrap service with logging middleware
	decorated := domain.WithLogging[*goods_receipt.GoodsReceipt]("goods-receipt")(service)

	return handlers.NewGoodsReceiptHandler(deps.BaseHandler, decorated)
}

// GoodsIssueRegistration wires the GoodsIssue document type.
type GoodsIssueRegistration struct{}

func (r *GoodsIssueRegistration) RoutePrefix() string       { return "goods-issue" }
func (r *GoodsIssueRegistration) Permission() string        { return "document:goods_issue" }
func (r *GoodsIssueRegistration) EntityName() string        { return "GoodsIssue" }
func (r *GoodsIssueRegistration) EntityLabel() string       { return "Реализация товаров" }
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

	// Decorator: wrap service with logging middleware
	decorated := domain.WithLogging[*goods_issue.GoodsIssue]("goods-issue")(service)

	return handlers.NewGoodsIssueHandler(deps.BaseHandler, decorated)
}
