package handlers

import (
	"context"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/command"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type CryptoProviderBalanceOperationHandler struct {
	*BaseDocumentHandler[*crypto_provider_balance_operation.CryptoProviderBalanceOperation, dto.CreateCryptoProviderBalanceOperationRequest, dto.UpdateCryptoProviderBalanceOperationRequest]
	base     *BaseHandler
	workflow *cryptoapp.ProviderBalanceWorkflowService
}

func NewCryptoProviderBalanceOperationHandler(base *BaseHandler, service domain.DocumentService[*crypto_provider_balance_operation.CryptoProviderBalanceOperation], workflow *cryptoapp.ProviderBalanceWorkflowService, movementProviders []entity.MovementProvider, movementRefResolver domain.RefResolver, settingsRepo settings.Repository) *CryptoProviderBalanceOperationHandler {
	cfg := BaseDocumentHandlerConfig[*crypto_provider_balance_operation.CryptoProviderBalanceOperation, dto.CreateCryptoProviderBalanceOperationRequest, dto.UpdateCryptoProviderBalanceOperationRequest]{
		Service: service, EntityName: "crypto_provider_balance_operation",
		MapCreateDTO: func(req dto.CreateCryptoProviderBalanceOperationRequest) (*crypto_provider_balance_operation.CryptoProviderBalanceOperation, error) {
			return req.ToEntity(), nil
		},
		MapUpdateDTO: func(req dto.UpdateCryptoProviderBalanceOperationRequest, existing *crypto_provider_balance_operation.CryptoProviderBalanceOperation) (*crypto_provider_balance_operation.CryptoProviderBalanceOperation, error) {
			req.ApplyTo(existing)
			return existing, nil
		},
		MapToDTO: func(doc *crypto_provider_balance_operation.CryptoProviderBalanceOperation) any {
			return dto.FromCryptoProviderBalanceOperation(doc)
		},
		ResolveRefs: func(context.Context, ...*crypto_provider_balance_operation.CryptoProviderBalanceOperation) (any, error) {
			return nil, nil
		},
		MapToDTOWithRefs: func(doc *crypto_provider_balance_operation.CryptoProviderBalanceOperation, _ any) any {
			return dto.FromCryptoProviderBalanceOperation(doc)
		},
		EnrichReadResponse: func(ctx context.Context, doc *crypto_provider_balance_operation.CryptoProviderBalanceOperation, _ any) (any, error) {
			availability, err := workflow.ActionAvailability(ctx, doc)
			if err != nil {
				return nil, err
			}
			return dto.CryptoProviderBalanceOperationDetail{CryptoProviderBalanceOperation: doc, ActionAvailability: availability}, nil
		},
		MovementProviders: movementProviders, MovementRefResolver: movementRefResolver, SettingsRepo: settingsRepo,
	}
	return &CryptoProviderBalanceOperationHandler{BaseDocumentHandler: NewBaseDocumentHandler(base, cfg), base: base, workflow: workflow}
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

	var req dto.PrepareProviderOpeningRequest
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
	var req dto.ProviderBalanceDecisionRequest
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
	var req dto.ProviderBalanceDecisionRequest
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
