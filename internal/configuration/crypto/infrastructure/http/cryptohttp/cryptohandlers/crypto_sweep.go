package cryptohandlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/http/handlers"
	"metapus/pkg/sdk/http/middleware"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/readaccess"
	"metapus/pkg/sdk/settings"
)

// CryptoSweepHandler handles HTTP requests for CryptoSweep documents.
// Sweep is a system document — read-only in API (created by Worker).
type CryptoSweepHandler struct {
	*handlers.BaseDocumentHandler[*crypto_sweep.CryptoSweep, cryptodto.CreateCryptoSweepRequest, cryptodto.UpdateCryptoSweepRequest]
	retryService       CryptoSweepRetryService
	relatedDocsHandler *handlers.RelatedDocumentsHandler
}

type CryptoSweepCardReader interface {
	ReadCardData(context.Context, *crypto_sweep.CryptoSweep) (crypto_document_repo.CryptoSweepCardData, error)
}

type CryptoSweepRetryService interface {
	RetryHeld(ctx context.Context, docID, actorID id.ID) error
}

func (h *CryptoSweepHandler) RegisterCustomDocumentRoutes(group *gin.RouterGroup, permission string) {
	group.POST("/:id/retry", middleware.RequirePermissionOrSystemAdmin(permission+":post"), h.Retry)
}

func (h *CryptoSweepHandler) Retry(c *gin.Context) {
	if h.retryService == nil {
		h.Error(c, apperror.NewInternal(nil))
		return
	}
	sweepID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid crypto sweep id"))
		return
	}
	if err := h.retryService.RetryHeld(c.Request.Context(), sweepID, parseContextUserID(c.Request.Context())); err != nil {
		h.Error(c, err)
		return
	}
	h.Success(c, "crypto sweep queued for retry")
}

func resolveCryptoSweepRefs(ctx context.Context, docs ...*crypto_sweep.CryptoSweep) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		cryptodto.CollectCryptoSweepRefs(resolver, doc)
	}
	refs, err := resolver.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// NewCryptoSweepHandler creates a new crypto sweep handler.
func NewCryptoSweepHandler(
	base *handlers.BaseHandler,
	service domain.DocumentService[*crypto_sweep.CryptoSweep],
	retryService CryptoSweepRetryService,
	cardReader CryptoSweepCardReader,
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *CryptoSweepHandler {
	cfg := handlers.BaseDocumentHandlerConfig[*crypto_sweep.CryptoSweep, cryptodto.CreateCryptoSweepRequest, cryptodto.UpdateCryptoSweepRequest]{
		Service:    service,
		EntityName: "crypto_sweep",
		MapCreateDTO: func(req cryptodto.CreateCryptoSweepRequest) (*crypto_sweep.CryptoSweep, error) {
			return req.ToEntity(), nil
		},
		MapUpdateDTO: func(req cryptodto.UpdateCryptoSweepRequest, existing *crypto_sweep.CryptoSweep) (*crypto_sweep.CryptoSweep, error) {
			req.ApplyTo(existing)
			return existing, nil
		},
		MapToDTO: func(entity *crypto_sweep.CryptoSweep) any {
			return cryptodto.FromCryptoSweep(entity)
		},
		ResolveRefs: resolveCryptoSweepRefs,
		MapToDTOWithRefs: func(entity *crypto_sweep.CryptoSweep, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return cryptodto.FromCryptoSweep(entity, resolvedRefs)
		},
		EnrichReadResponse: func(ctx context.Context, doc *crypto_sweep.CryptoSweep, response any) (any, error) {
			facts, err := cardReader.ReadCardData(ctx, doc)
			if err != nil {
				return nil, err
			}
			if err := restrictCryptoSweepCardRefs(ctx, &facts); err != nil {
				return nil, err
			}
			return cryptodto.FromCryptoSweepDetail(ctx, doc, response.(*cryptodto.CryptoSweepResponse), facts), nil
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &CryptoSweepHandler{
		BaseDocumentHandler: handlers.NewBaseDocumentHandler(base, cfg),
		retryService:        retryService,
	}
	if relatedDocFinder != nil {
		h.relatedDocsHandler = handlers.NewRelatedDocumentsHandler(relatedDocFinder, crypto_sweep.DocumentType)
	}
	return h
}

// Related catalog and rental presentations must pass their own row policies.
// The source sweep authorization alone does not grant access to those entities.
func restrictCryptoSweepCardRefs(ctx context.Context, facts *crypto_document_repo.CryptoSweepCardData) error {
	reads, ok := readaccess.FromContext(ctx)
	if !ok {
		facts.Tokens = nil
		facts.Wallets = nil
		facts.ResourceCosts = nil
		return nil
	}
	allowed := func(key string, ids []id.ID) (map[id.ID]bool, error) {
		result := make(map[id.ID]bool)
		if _, err := reads.Require(ctx, key); err != nil {
			return result, nil
		}
		rows, err := reads.ReadIDs(ctx, key, ids)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ID] = true
		}
		return result, nil
	}
	tokenIDs := make([]id.ID, 0, len(facts.Tokens))
	for _, token := range facts.Tokens {
		tokenIDs = append(tokenIDs, token.ID)
	}
	tokens, err := allowed("token", tokenIDs)
	if err != nil {
		return err
	}
	visibleTokens := facts.Tokens[:0]
	for _, token := range facts.Tokens {
		if tokens[token.ID] {
			visibleTokens = append(visibleTokens, token)
		}
	}
	facts.Tokens = visibleTokens
	walletIDs := make([]id.ID, 0, len(facts.Wallets))
	for _, wallet := range facts.Wallets {
		walletIDs = append(walletIDs, wallet.ID)
	}
	wallets, err := allowed("wallet", walletIDs)
	if err != nil {
		return err
	}
	visibleWallets := facts.Wallets[:0]
	for _, wallet := range facts.Wallets {
		if wallets[wallet.ID] {
			visibleWallets = append(visibleWallets, wallet)
		}
	}
	facts.Wallets = visibleWallets
	rentalIDs := make([]id.ID, 0, len(facts.ResourceCosts))
	for _, cost := range facts.ResourceCosts {
		rentalIDs = append(rentalIDs, cost.RentalID)
	}
	rentals, err := allowed("crypto_resource_rental", rentalIDs)
	if err != nil {
		return err
	}
	visibleCosts := facts.ResourceCosts[:0]
	for _, cost := range facts.ResourceCosts {
		if rentals[cost.RentalID] {
			visibleCosts = append(visibleCosts, cost)
		}
	}
	facts.ResourceCosts = visibleCosts
	return nil
}

func (h *CryptoSweepHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
