package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/entity"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoPaymentHandler handles HTTP requests for CryptoPayment documents.
// CryptoPayments are read-only in the API — created by the chain watcher.
type CryptoPaymentHandler struct {
	*BaseDocumentHandler[*crypto_payment.CryptoPayment, dto.CreateCryptoPaymentRequest, dto.UpdateCryptoPaymentRequest]
	service            domain.DocumentService[*crypto_payment.CryptoPayment]
	relatedDocsHandler *RelatedDocumentsHandler
}

// resolveCryptoPaymentRefs batch-resolves all reference IDs for CryptoPayment documents.
func resolveCryptoPaymentRefs(ctx context.Context, docs ...*crypto_payment.CryptoPayment) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		dto.CollectCryptoPaymentRefs(resolver, doc)
	}

	pool := tenant.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// NewCryptoPaymentHandler creates a new crypto payment handler.
func NewCryptoPaymentHandler(
	base *BaseHandler,
	service domain.DocumentService[*crypto_payment.CryptoPayment],
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *CryptoPaymentHandler {
	cfg := BaseDocumentHandlerConfig[*crypto_payment.CryptoPayment, dto.CreateCryptoPaymentRequest, dto.UpdateCryptoPaymentRequest]{
		Service:    service,
		EntityName: "crypto_payment",
		MapCreateDTO: func(req dto.CreateCryptoPaymentRequest) *crypto_payment.CryptoPayment {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateCryptoPaymentRequest, existing *crypto_payment.CryptoPayment) *crypto_payment.CryptoPayment {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *crypto_payment.CryptoPayment) any {
			return dto.FromCryptoPayment(entity)
		},
		ResolveRefs: resolveCryptoPaymentRefs,
		MapToDTOWithRefs: func(entity *crypto_payment.CryptoPayment, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return dto.FromCryptoPayment(entity, resolvedRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &CryptoPaymentHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		service:             service,
	}

	// Related documents
	if relatedDocFinder != nil {
		h.relatedDocsHandler = NewRelatedDocumentsHandler(relatedDocFinder, "CryptoPayment")
	}

	return h
}

// GetRelatedDocuments handles GET /document/crypto-payment/:id/related-documents.
func (h *CryptoPaymentHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
