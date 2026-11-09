package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
)

const cryptoNetworkOperationUnlockAction = "crypto.network_operation.allow_editing"

type CryptoNetworkOperationHandler struct {
	*BaseDocumentHandler[*crypto_network_operation.CryptoNetworkOperation, dto.CreateCryptoNetworkOperationRequest, dto.UpdateCryptoNetworkOperationRequest]
	protection         *cryptoapp.NetworkOperationProtectionService
	stepUpGuard        gin.HandlerFunc
	relatedDocsHandler *RelatedDocumentsHandler
}

func (h *CryptoNetworkOperationHandler) RegisterCustomDocumentRoutes(group *gin.RouterGroup, permission string) {
	allowHandlers := []gin.HandlerFunc{
		middleware.RequirePermission(cryptoNetworkOperationUnlockAction),
		middleware.RequireSystemAdmin(),
	}
	if h.stepUpGuard != nil {
		allowHandlers = append(allowHandlers, h.stepUpGuard)
	}
	allowHandlers = append(allowHandlers, h.AllowEditing)
	group.POST("/:id/allow-editing", allowHandlers...)
	group.POST("/:id/deny-editing",
		middleware.RequirePermission(cryptoNetworkOperationUnlockAction),
		middleware.RequireSystemAdmin(),
		h.DenyEditing,
	)
}

func (h *CryptoNetworkOperationHandler) AllowEditing(c *gin.Context) {
	h.changeProtection(c, true)
}

func (h *CryptoNetworkOperationHandler) DenyEditing(c *gin.Context) {
	h.changeProtection(c, false)
}

func (h *CryptoNetworkOperationHandler) changeProtection(c *gin.Context, allowed bool) {
	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid crypto network operation id"))
		return
	}
	doc, err := h.protection.SetEditingAllowed(c.Request.Context(), docID, allowed)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, dto.FromCryptoNetworkOperation(doc))
}

func NewCryptoNetworkOperationHandler(
	base *BaseHandler,
	service domain.DocumentService[*crypto_network_operation.CryptoNetworkOperation],
	editing cryptoapp.CryptoNetworkOperationEditor,
	stepUpGuard gin.HandlerFunc,
	eventWriter eventlog.Writer,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
	relatedDocFinder domain.RelatedDocFinder,
) *CryptoNetworkOperationHandler {
	cfg := BaseDocumentHandlerConfig[*crypto_network_operation.CryptoNetworkOperation, dto.CreateCryptoNetworkOperationRequest, dto.UpdateCryptoNetworkOperationRequest]{
		Service: service, EntityName: "crypto_network_operation",
		MapCreateDTO: func(req dto.CreateCryptoNetworkOperationRequest) (*crypto_network_operation.CryptoNetworkOperation, error) {
			return req.ToEntity(), nil
		},
		MapUpdateDTO: func(req dto.UpdateCryptoNetworkOperationRequest, existing *crypto_network_operation.CryptoNetworkOperation) (*crypto_network_operation.CryptoNetworkOperation, error) {
			req.ApplyTo(existing)
			return existing, nil
		},
		MapToDTO: func(doc *crypto_network_operation.CryptoNetworkOperation) any {
			return dto.FromCryptoNetworkOperation(doc)
		},
		ResolveRefs: func(context.Context, ...*crypto_network_operation.CryptoNetworkOperation) (any, error) {
			return nil, nil
		},
		MapToDTOWithRefs: func(doc *crypto_network_operation.CryptoNetworkOperation, _ any) any {
			return dto.FromCryptoNetworkOperation(doc)
		},
		MovementProviders: movementProviders, MovementRefResolver: movementRefResolver, SettingsRepo: settingsRepo,
	}
	h := &CryptoNetworkOperationHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		protection:          cryptoapp.NewNetworkOperationProtectionService(postgres.NewContextTxManager(), editing, eventWriter),
		stepUpGuard:         stepUpGuard,
	}
	if relatedDocFinder != nil {
		h.relatedDocsHandler = NewRelatedDocumentsHandler(relatedDocFinder, crypto_network_operation.DocumentType)
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
