package cryptohandlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/entity"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/tenantdb"
)

// CryptoWithdrawalHandler handles HTTP requests for CryptoWithdrawal documents.
type CryptoWithdrawalHandler struct {
	*handlers.BaseDocumentHandler[*crypto_withdrawal.CryptoWithdrawal, cryptodto.CreateCryptoWithdrawalRequest, cryptodto.UpdateCryptoWithdrawalRequest]
	relatedDocsHandler *handlers.RelatedDocumentsHandler
}

func resolveCryptoWithdrawalRefs(ctx context.Context, docs ...*crypto_withdrawal.CryptoWithdrawal) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		cryptodto.CollectCryptoWithdrawalRefs(resolver, doc)
	}
	pool := tenantdb.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// NewCryptoWithdrawalHandler creates a new crypto withdrawal handler.
func NewCryptoWithdrawalHandler(
	base *handlers.BaseHandler,
	service domain.DocumentService[*crypto_withdrawal.CryptoWithdrawal],
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *CryptoWithdrawalHandler {
	cfg := handlers.BaseDocumentHandlerConfig[*crypto_withdrawal.CryptoWithdrawal, cryptodto.CreateCryptoWithdrawalRequest, cryptodto.UpdateCryptoWithdrawalRequest]{
		Service:    service,
		EntityName: "crypto_withdrawal",
		MapCreateDTO: func(req cryptodto.CreateCryptoWithdrawalRequest) (*crypto_withdrawal.CryptoWithdrawal, error) {
			return req.ToEntity(), nil
		},
		MapUpdateDTO: func(req cryptodto.UpdateCryptoWithdrawalRequest, existing *crypto_withdrawal.CryptoWithdrawal) (*crypto_withdrawal.CryptoWithdrawal, error) {
			req.ApplyTo(existing)
			return existing, nil
		},
		MapToDTO: func(entity *crypto_withdrawal.CryptoWithdrawal) any {
			return cryptodto.FromCryptoWithdrawal(entity)
		},
		ResolveRefs: resolveCryptoWithdrawalRefs,
		MapToDTOWithRefs: func(entity *crypto_withdrawal.CryptoWithdrawal, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return cryptodto.FromCryptoWithdrawal(entity, resolvedRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &CryptoWithdrawalHandler{
		BaseDocumentHandler: handlers.NewBaseDocumentHandler(base, cfg),
	}
	if relatedDocFinder != nil {
		h.relatedDocsHandler = handlers.NewRelatedDocumentsHandler(relatedDocFinder, crypto_withdrawal.DocumentType)
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
