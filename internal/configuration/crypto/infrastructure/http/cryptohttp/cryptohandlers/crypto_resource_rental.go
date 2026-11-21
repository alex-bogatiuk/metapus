package cryptohandlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/documents/crypto_resource_rental"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type CryptoResourceRentalHandler struct {
	*handlers.BaseDocumentHandler[*crypto_resource_rental.CryptoResourceRental, struct{}, struct{}]
	relatedDocsHandler *handlers.RelatedDocumentsHandler
	workflow           *cryptoapp.ResourceRentalWorkflowService
}

func (h *CryptoResourceRentalHandler) RegisterCustomDocumentRoutes(group *gin.RouterGroup, permission string) {
	group.GET("/metrics", middleware.RequirePermission(permission+":read"), h.GetMetrics)
	group.GET("/provider-balance-incidents", middleware.RequirePermission(permission+":read"), h.ListProviderBalanceIncidents)
	group.POST("/provider-balance-incidents/:incidentId/acknowledge", middleware.RequirePermission(permission+":post"), h.AcknowledgeProviderBalanceIncident)
	group.GET("/:id/allocations", middleware.RequirePermission(permission+":read"), h.GetAllocations)
	group.POST("/:id/reclaim", middleware.RequirePermission(permission+":post"), h.Reclaim)
	group.POST("/:id/retry", middleware.RequirePermission(permission+":post"), h.Retry)
}

func (h *CryptoResourceRentalHandler) GetMetrics(c *gin.Context) {
	value, err := h.workflow.Metrics(c.Request.Context())
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, value)
}

func (h *CryptoResourceRentalHandler) ListProviderBalanceIncidents(c *gin.Context) {
	items, err := h.workflow.ListIncidents(c.Request.Context())
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, cryptodto.ProviderBalanceIncidentsResponse{Items: items})
}

func (h *CryptoResourceRentalHandler) AcknowledgeProviderBalanceIncident(c *gin.Context) {
	incidentID, err := id.Parse(c.Param("incidentId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid provider balance incident id"))
		return
	}
	if err := h.workflow.AcknowledgeIncident(c.Request.Context(), incidentID); err != nil {
		h.Error(c, err)
		return
	}
	h.Success(c, "provider balance incident acknowledged")
}

func (h *CryptoResourceRentalHandler) GetAllocations(c *gin.Context) {
	rentalID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid resource rental id"))
		return
	}
	items, err := h.workflow.ListAllocations(c.Request.Context(), rentalID)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, cryptodto.ResourceRentalAllocationsResponse{Items: items})
}

func (h *CryptoResourceRentalHandler) Reclaim(c *gin.Context) {
	rentalID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid resource rental id"))
		return
	}
	if err := h.workflow.Reclaim(c.Request.Context(), rentalID, parseContextUserID(c.Request.Context())); err != nil {
		h.Error(c, err)
		return
	}
	h.Success(c, "resource rental queued for reclaim")
}

func (h *CryptoResourceRentalHandler) Retry(c *gin.Context) {
	rentalID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid resource rental id"))
		return
	}
	if err := h.workflow.Retry(c.Request.Context(), rentalID, parseContextUserID(c.Request.Context())); err != nil {
		h.Error(c, err)
		return
	}
	h.Success(c, "resource rental queued for retry")
}

func NewCryptoResourceRentalHandler(
	base *handlers.BaseHandler,
	service domain.DocumentService[*crypto_resource_rental.CryptoResourceRental],
	workflow *cryptoapp.ResourceRentalWorkflowService,
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *CryptoResourceRentalHandler {
	h := &CryptoResourceRentalHandler{
		BaseDocumentHandler: handlers.NewReadOnlyDocumentHandler(base, service, crypto_resource_rental.Definition.Key,
			handlers.ReadOnlyDocumentPresenter[*crypto_resource_rental.CryptoResourceRental, cryptodto.CryptoResourceRentalResponse]{
				Collect: cryptodto.CollectCryptoResourceRentalRefs, Present: cryptodto.FromCryptoResourceRental,
			},
			movementProviders, movementRefResolver, settingsRepo),
		workflow: workflow,
	}
	if relatedDocFinder != nil {
		h.relatedDocsHandler = handlers.NewRelatedDocumentsHandler(relatedDocFinder, "CryptoResourceRental")
	}
	return h
}

func (h *CryptoResourceRentalHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
