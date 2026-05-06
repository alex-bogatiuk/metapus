package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/entity"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoSweepHandler handles HTTP requests for CryptoSweep documents.
// Sweep is a system document — read-only in API (created by Worker).
type CryptoSweepHandler struct {
	*BaseDocumentHandler[*crypto_sweep.CryptoSweep, dto.CreateCryptoSweepRequest, dto.UpdateCryptoSweepRequest]
	service            domain.DocumentService[*crypto_sweep.CryptoSweep]
	relatedDocsHandler *RelatedDocumentsHandler
}

func resolveCryptoSweepRefs(ctx context.Context, docs ...*crypto_sweep.CryptoSweep) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		dto.CollectCryptoSweepRefs(resolver, doc)
	}
	pool := tenant.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// NewCryptoSweepHandler creates a new crypto sweep handler.
func NewCryptoSweepHandler(
	base *BaseHandler,
	service domain.DocumentService[*crypto_sweep.CryptoSweep],
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *CryptoSweepHandler {
	cfg := BaseDocumentHandlerConfig[*crypto_sweep.CryptoSweep, dto.CreateCryptoSweepRequest, dto.UpdateCryptoSweepRequest]{
		Service:    service,
		EntityName: "crypto_sweep",
		MapCreateDTO: func(req dto.CreateCryptoSweepRequest) *crypto_sweep.CryptoSweep {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateCryptoSweepRequest, existing *crypto_sweep.CryptoSweep) *crypto_sweep.CryptoSweep {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *crypto_sweep.CryptoSweep) any {
			return dto.FromCryptoSweep(entity)
		},
		ResolveRefs: resolveCryptoSweepRefs,
		MapToDTOWithRefs: func(entity *crypto_sweep.CryptoSweep, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return dto.FromCryptoSweep(entity, resolvedRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &CryptoSweepHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		service:             service,
	}
	if relatedDocFinder != nil {
		h.relatedDocsHandler = NewRelatedDocumentsHandler(relatedDocFinder, "CryptoSweep")
	}
	return h
}

func (h *CryptoSweepHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
