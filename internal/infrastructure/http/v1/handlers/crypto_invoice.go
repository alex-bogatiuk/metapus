package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/entity"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoInvoiceHandler handles HTTP requests for CryptoInvoice documents.
// Standard CRUD/posting methods are handled by BaseDocumentHandler.
type CryptoInvoiceHandler struct {
	*BaseDocumentHandler[*crypto_invoice.CryptoInvoice, dto.CreateCryptoInvoiceRequest, dto.UpdateCryptoInvoiceRequest]
	service            domain.DocumentService[*crypto_invoice.CryptoInvoice]
	relatedDocsHandler *RelatedDocumentsHandler
}

// resolveCryptoInvoiceRefs batch-resolves all reference IDs for CryptoInvoice documents.
func resolveCryptoInvoiceRefs(ctx context.Context, docs ...*crypto_invoice.CryptoInvoice) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		dto.CollectCryptoInvoiceRefs(resolver, doc)
	}

	pool := tenant.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// NewCryptoInvoiceHandler creates a new crypto invoice handler.
func NewCryptoInvoiceHandler(
	base *BaseHandler,
	service domain.DocumentService[*crypto_invoice.CryptoInvoice],
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *CryptoInvoiceHandler {
	cfg := BaseDocumentHandlerConfig[*crypto_invoice.CryptoInvoice, dto.CreateCryptoInvoiceRequest, dto.UpdateCryptoInvoiceRequest]{
		Service:    service,
		EntityName: "crypto_invoice",
		MapCreateDTO: func(req dto.CreateCryptoInvoiceRequest) *crypto_invoice.CryptoInvoice {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateCryptoInvoiceRequest, existing *crypto_invoice.CryptoInvoice) *crypto_invoice.CryptoInvoice {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *crypto_invoice.CryptoInvoice) any {
			return dto.FromCryptoInvoice(entity, nil)
		},
		IsPostImmediately: func(req dto.CreateCryptoInvoiceRequest) bool {
			return req.PostImmediately
		},
		ResolveRefs: resolveCryptoInvoiceRefs,
		MapToDTOWithRefs: func(entity *crypto_invoice.CryptoInvoice, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return dto.FromCryptoInvoice(entity, resolvedRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &CryptoInvoiceHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		service:             service,
	}

	// Related documents (optional)
	if relatedDocFinder != nil {
		h.relatedDocsHandler = NewRelatedDocumentsHandler(relatedDocFinder, "CryptoInvoice")
	}

	return h
}

// GetRelatedDocuments handles GET /document/crypto-invoice/:id/related-documents.
func (h *CryptoInvoiceHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
