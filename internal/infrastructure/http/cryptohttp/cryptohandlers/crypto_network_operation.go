package cryptohandlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/entity"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
)

// CryptoNetworkOperationHandler serves network operations read-only: list,
// card, register movements and related documents.
type CryptoNetworkOperationHandler struct {
	*handlers.BaseDocumentHandler[*crypto_network_operation.CryptoNetworkOperation, struct{}, struct{}]
	relatedDocsHandler *handlers.RelatedDocumentsHandler
}

func NewCryptoNetworkOperationHandler(
	base *handlers.BaseHandler,
	service domain.DocumentService[*crypto_network_operation.CryptoNetworkOperation],
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
	relatedDocFinder domain.RelatedDocFinder,
) *CryptoNetworkOperationHandler {
	h := &CryptoNetworkOperationHandler{
		BaseDocumentHandler: handlers.NewReadOnlyDocumentHandler(base, service, crypto_network_operation.Definition.Key,
			handlers.ReadOnlyDocumentPresenter[*crypto_network_operation.CryptoNetworkOperation, dto.CryptoNetworkOperationResponse]{
				Collect: dto.CollectCryptoNetworkOperationRefs, Present: dto.FromCryptoNetworkOperation,
			},
			movementProviders, movementRefResolver, settingsRepo),
	}
	if relatedDocFinder != nil {
		h.relatedDocsHandler = handlers.NewRelatedDocumentsHandler(relatedDocFinder, crypto_network_operation.DocumentType)
	}
	return h
}

func (h *CryptoNetworkOperationHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
