package cryptohandlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/core/entity"
	"metapus/internal/domain"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/handlers"
	printadapter "metapus/internal/infrastructure/printing"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/tenantdb"
)

// CryptoInvoiceHandler handles HTTP requests for CryptoInvoice documents.
// Standard CRUD/posting methods are handled by BaseDocumentHandler.
type CryptoInvoiceHandler struct {
	*handlers.BaseDocumentHandler[*crypto_invoice.CryptoInvoice, cryptodto.CreateCryptoInvoiceRequest, cryptodto.UpdateCryptoInvoiceRequest]
	relatedDocsHandler *handlers.RelatedDocumentsHandler
	print              *handlers.DocumentPrintHandler[*crypto_invoice.CryptoInvoice]
}

// DocumentPrintDeps are the shared print engine parts a document handler uses.
type DocumentPrintDeps struct {
	Registry    *printing.PrintFormRegistry
	Renderer    *printadapter.Renderer
	PDFRenderer printing.PDFRenderer
}

// resolveCryptoInvoiceRefs batch-resolves all reference IDs for CryptoInvoice documents.
func resolveCryptoInvoiceRefs(ctx context.Context, docs ...*crypto_invoice.CryptoInvoice) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		cryptodto.CollectCryptoInvoiceRefs(resolver, doc)
	}

	pool := tenantdb.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// NewCryptoInvoiceHandler creates a new crypto invoice handler.
func NewCryptoInvoiceHandler(
	base *handlers.BaseHandler,
	service domain.DocumentService[*crypto_invoice.CryptoInvoice],
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
	printDeps DocumentPrintDeps,
) *CryptoInvoiceHandler {
	cfg := handlers.BaseDocumentHandlerConfig[*crypto_invoice.CryptoInvoice, cryptodto.CreateCryptoInvoiceRequest, cryptodto.UpdateCryptoInvoiceRequest]{
		Service:    service,
		EntityName: "crypto_invoice",
		MapCreateDTO: func(req cryptodto.CreateCryptoInvoiceRequest) (*crypto_invoice.CryptoInvoice, error) {
			return req.ToEntity(), nil
		},
		MapUpdateDTO: func(req cryptodto.UpdateCryptoInvoiceRequest, existing *crypto_invoice.CryptoInvoice) (*crypto_invoice.CryptoInvoice, error) {
			req.ApplyTo(existing)
			return existing, nil
		},
		MapToDTO: func(entity *crypto_invoice.CryptoInvoice) any {
			return cryptodto.FromCryptoInvoice(entity, nil)
		},
		IsPostImmediately: func(req cryptodto.CreateCryptoInvoiceRequest) bool {
			return req.PostImmediately
		},
		ResolveRefs: resolveCryptoInvoiceRefs,
		MapToDTOWithRefs: func(entity *crypto_invoice.CryptoInvoice, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return cryptodto.FromCryptoInvoice(entity, resolvedRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	registry := printDeps.Registry
	if registry == nil {
		registry = printing.NewPrintFormRegistry()
		for _, form := range CryptoInvoicePrintForms() {
			registry.Register(crypto_invoice.Definition.Key, form)
		}
	}
	h := &CryptoInvoiceHandler{
		BaseDocumentHandler: handlers.NewBaseDocumentHandler(base, cfg),
		print: handlers.NewDocumentPrintHandler(base, handlers.DocumentPrintHandlerConfig[*crypto_invoice.CryptoInvoice]{
			Service:        service,
			EntityName:     "crypto_invoice",
			DocType:        crypto_invoice.Definition.Key,
			Registry:       registry,
			Renderer:       printDeps.Renderer,
			PDFRenderer:    printDeps.PDFRenderer,
			BuildPrintData: buildCryptoInvoicePrintData,
		}),
	}

	// Related documents (optional)
	if relatedDocFinder != nil {
		h.relatedDocsHandler = handlers.NewRelatedDocumentsHandler(relatedDocFinder, "CryptoInvoice")
	}

	return h
}

// CryptoInvoicePrintForms lists the invoice print forms; the first is the default.
func CryptoInvoicePrintForms() []printing.PrintFormDef {
	return []printing.PrintFormDef{{
		Name:      "standard",
		Label:     "Инвойс",
		Template:  printing.TableTemplate,
		PaperSize: "A4",
		Category:  printing.CategoryStandard,
	}}
}

// Print handles GET /document/crypto-invoice/:id/print.
func (h *CryptoInvoiceHandler) Print(c *gin.Context) { h.print.Print(c) }

// ListPrintForms handles GET /document/crypto-invoice/print-forms.
func (h *CryptoInvoiceHandler) ListPrintForms(c *gin.Context) { h.print.ListPrintForms(c) }

// GetRelatedDocuments handles GET /document/crypto-invoice/:id/related-documents.
func (h *CryptoInvoiceHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
