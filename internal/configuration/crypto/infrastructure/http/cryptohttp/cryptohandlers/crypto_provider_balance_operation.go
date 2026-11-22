package cryptohandlers

import (
	"context"

	"github.com/gin-gonic/gin"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/command"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/http/handlers"
	"metapus/pkg/sdk/http/middleware"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/settings"
)

type CryptoProviderBalanceOperationHandler struct {
	*handlers.BaseDocumentHandler[*crypto_provider_balance_operation.CryptoProviderBalanceOperation, struct{}, struct{}]
	base     *handlers.BaseHandler
	workflow *cryptoapp.ProviderBalanceWorkflowService
}

// NewCryptoProviderBalanceOperationHandler serves the operations read-only;
// they are prepared, approved and rejected only through the workflow routes.
func NewCryptoProviderBalanceOperationHandler(base *handlers.BaseHandler, service domain.DocumentService[*crypto_provider_balance_operation.CryptoProviderBalanceOperation], workflow *cryptoapp.ProviderBalanceWorkflowService, movementProviders []entity.MovementProvider, movementRefResolver domain.RefResolver, settingsRepo settings.Repository) *CryptoProviderBalanceOperationHandler {
	presenter := handlers.ReadOnlyDocumentPresenter[*crypto_provider_balance_operation.CryptoProviderBalanceOperation, cryptodto.CryptoProviderBalanceOperationResponse]{
		Collect: cryptodto.CollectCryptoProviderBalanceOperationRefs, Present: cryptodto.FromCryptoProviderBalanceOperation,
		Enrich: func(ctx context.Context, doc *crypto_provider_balance_operation.CryptoProviderBalanceOperation, response cryptodto.CryptoProviderBalanceOperationResponse) (any, error) {
			availability, err := workflow.ActionAvailability(ctx, doc)
			if err != nil {
				return nil, err
			}
			return cryptodto.CryptoProviderBalanceOperationDetail{CryptoProviderBalanceOperationResponse: response, ActionAvailability: availability}, nil
		},
	}
	return &CryptoProviderBalanceOperationHandler{
		BaseDocumentHandler: handlers.NewReadOnlyDocumentHandler(base, service, crypto_provider_balance_operation.Definition.Key, presenter,
			movementProviders, movementRefResolver, settingsRepo),
		base: base, workflow: workflow,
	}
}

func (h *CryptoProviderBalanceOperationHandler) RegisterCustomDocumentRoutes(group *gin.RouterGroup, permission string) {
	group.POST("/provider-balance-incidents/:incidentId/prepare-opening", middleware.RequirePermissionOrSystemAdmin(permission+":create"), h.PrepareOpeningFromIncident)
	group.POST("/:id/approve", middleware.RequirePermissionOrSystemAdmin(permission+":approve"), h.Approve)
	group.POST("/:id/reject", middleware.RequirePermissionOrSystemAdmin(permission+":reject"), h.Reject)
}

func (h *CryptoProviderBalanceOperationHandler) PrepareOpeningFromIncident(c *gin.Context) {
	incidentID, err := id.Parse(c.Param("incidentId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid provider balance incident id"))
		return
	}

	var req cryptodto.PrepareProviderOpeningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.base.Error(c, apperror.NewValidation("observationId and reason are required"))
		return
	}
	result, err := h.workflow.PrepareOpening(c.Request.Context(), cryptoapp.PrepareProviderOpeningCommand{IncidentID: incidentID, ObservationID: req.ObservationID, Reason: req.Reason, IdempotencyKey: c.GetHeader("Idempotency-Key")})
	h.writeCommandResult(c, result, err)
}

func (h *CryptoProviderBalanceOperationHandler) Approve(c *gin.Context) {
	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid provider balance operation id"))
		return
	}
	var req cryptodto.ProviderBalanceDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.base.Error(c, apperror.NewValidation("version and decision reason are required"))
		return
	}
	result, err := h.workflow.Approve(c.Request.Context(), cryptoapp.ProviderBalanceDecisionCommand{OperationID: docID, Version: req.Version, Reason: req.Reason, IdempotencyKey: c.GetHeader("Idempotency-Key")})
	h.writeCommandResult(c, result, err)
}

func (h *CryptoProviderBalanceOperationHandler) Reject(c *gin.Context) {
	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid provider balance operation id"))
		return
	}
	var req cryptodto.ProviderBalanceDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.base.Error(c, apperror.NewValidation("rejection reason is required"))
		return
	}
	result, err := h.workflow.Reject(c.Request.Context(), cryptoapp.ProviderBalanceDecisionCommand{OperationID: docID, Version: req.Version, Reason: req.Reason, IdempotencyKey: c.GetHeader("Idempotency-Key")})
	h.writeCommandResult(c, result, err)
}

func (h *CryptoProviderBalanceOperationHandler) writeCommandResult(c *gin.Context, result command.Execution, err error) {
	if err != nil {
		h.base.Error(c, err)
		return
	}
	if result.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	h.base.OK(c, result.Result.Payload)
}

func (h *CryptoProviderBalanceOperationHandler) Unpost(c *gin.Context) {
	_ = c.Error(apperror.NewMethodNotAllowed("provider balance operations are immutable after posting"))
	c.Abort()
}
