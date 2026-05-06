package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/entity"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoWithdrawalHandler handles HTTP requests for CryptoWithdrawal documents.
type CryptoWithdrawalHandler struct {
	*BaseDocumentHandler[*crypto_withdrawal.CryptoWithdrawal, dto.CreateCryptoWithdrawalRequest, dto.UpdateCryptoWithdrawalRequest]
	service            domain.DocumentService[*crypto_withdrawal.CryptoWithdrawal]
	relatedDocsHandler *RelatedDocumentsHandler
}

func resolveCryptoWithdrawalRefs(ctx context.Context, docs ...*crypto_withdrawal.CryptoWithdrawal) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		dto.CollectCryptoWithdrawalRefs(resolver, doc)
	}
	pool := tenant.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// NewCryptoWithdrawalHandler creates a new crypto withdrawal handler.
func NewCryptoWithdrawalHandler(
	base *BaseHandler,
	service domain.DocumentService[*crypto_withdrawal.CryptoWithdrawal],
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *CryptoWithdrawalHandler {
	cfg := BaseDocumentHandlerConfig[*crypto_withdrawal.CryptoWithdrawal, dto.CreateCryptoWithdrawalRequest, dto.UpdateCryptoWithdrawalRequest]{
		Service:    service,
		EntityName: "crypto_withdrawal",
		MapCreateDTO: func(req dto.CreateCryptoWithdrawalRequest) *crypto_withdrawal.CryptoWithdrawal {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateCryptoWithdrawalRequest, existing *crypto_withdrawal.CryptoWithdrawal) *crypto_withdrawal.CryptoWithdrawal {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *crypto_withdrawal.CryptoWithdrawal) any {
			return dto.FromCryptoWithdrawal(entity)
		},
		ResolveRefs: resolveCryptoWithdrawalRefs,
		MapToDTOWithRefs: func(entity *crypto_withdrawal.CryptoWithdrawal, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return dto.FromCryptoWithdrawal(entity, resolvedRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &CryptoWithdrawalHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		service:             service,
	}
	if relatedDocFinder != nil {
		h.relatedDocsHandler = NewRelatedDocumentsHandler(relatedDocFinder, "CryptoWithdrawal")
	}
	return h
}

func (h *CryptoWithdrawalHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
