package cryptohandlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/http/handlers"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/settings"
	"metapus/pkg/sdk/tenantdb"
)

// CryptoPaymentHandler handles HTTP requests for CryptoPayment documents.
// CryptoPayments are read-only in the API — created by the chain watcher.
type CryptoPaymentHandler struct {
	*handlers.BaseDocumentHandler[*crypto_payment.CryptoPayment, cryptodto.CreateCryptoPaymentRequest, cryptodto.UpdateCryptoPaymentRequest]
	relatedDocsHandler *handlers.RelatedDocumentsHandler
}

// resolveCryptoPaymentRefs batch-resolves all reference IDs for CryptoPayment documents.
func resolveCryptoPaymentRefs(ctx context.Context, docs ...*crypto_payment.CryptoPayment) (any, error) {
	ids := make([]id.ID, 0, len(docs))
	for _, item := range docs {
		ids = append(ids, item.ID)
	}
	views, viewErr := crypto_repo.NewAMLRepository().ReadPaymentRiskDisplays(ctx, ids)
	if viewErr != nil {
		return nil, viewErr
	}
	for _, item := range docs {
		item.RiskDisplay = views[item.ID]
	}

	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		cryptodto.CollectCryptoPaymentRefs(resolver, doc)
	}

	pool := tenantdb.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// NewCryptoPaymentHandler creates a new crypto payment handler.
func NewCryptoPaymentHandler(
	base *handlers.BaseHandler,
	service domain.DocumentService[*crypto_payment.CryptoPayment],
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *CryptoPaymentHandler {
	cfg := handlers.BaseDocumentHandlerConfig[*crypto_payment.CryptoPayment, cryptodto.CreateCryptoPaymentRequest, cryptodto.UpdateCryptoPaymentRequest]{
		Service:    service,
		EntityName: "crypto_payment",
		MapCreateDTO: func(req cryptodto.CreateCryptoPaymentRequest) (*crypto_payment.CryptoPayment, error) {
			return req.ToEntity(), nil
		},
		MapUpdateDTO: func(req cryptodto.UpdateCryptoPaymentRequest, existing *crypto_payment.CryptoPayment) (*crypto_payment.CryptoPayment, error) {
			req.ApplyTo(existing)
			return existing, nil
		},
		MapToDTO: func(entity *crypto_payment.CryptoPayment) any {
			return cryptodto.FromCryptoPayment(entity)
		},
		ResolveRefs: resolveCryptoPaymentRefs,
		MapToDTOWithRefs: func(entity *crypto_payment.CryptoPayment, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return cryptodto.FromCryptoPayment(entity, resolvedRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &CryptoPaymentHandler{
		BaseDocumentHandler: handlers.NewBaseDocumentHandler(base, cfg),
	}

	// Related documents
	if relatedDocFinder != nil {
		h.relatedDocsHandler = handlers.NewRelatedDocumentsHandler(relatedDocFinder, "CryptoPayment")
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
