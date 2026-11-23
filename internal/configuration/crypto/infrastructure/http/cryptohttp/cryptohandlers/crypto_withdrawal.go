package cryptohandlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/handlers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/settings"
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
	refs, err := resolver.Resolve(ctx)
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
