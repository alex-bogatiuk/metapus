package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/v1/dto"
)

// BaseDocumentHandler provides generic HTTP handlers for document entities.
// In Database-per-Tenant architecture, tenantID is not needed (isolation is physical).
type BaseDocumentHandler[T any, CreateDTO any, UpdateDTO any] struct {
	*BaseHandler
	service    domain.DocumentService[T]
	entityName string

	// Mapper functions
	mapCreateDTO      func(dto CreateDTO) T
	mapUpdateDTO      func(dto UpdateDTO, existing T) T
	mapToDTO          func(entity T) any
	isPostImmediately func(dto CreateDTO) bool

	// ResolveRefs batch-resolves FK → display names. Returns an opaque refs bag.
	// If nil, no resolution is performed.
	resolveRefs func(ctx context.Context, entities ...T) (any, error)

	// mapToDTOWithRefs is an enhanced mapper that receives the resolved refs bag.
	// Used instead of mapToDTO when resolveRefs is configured.
	mapToDTOWithRefs func(entity T, refs any) any

	// Movement providers for the document
	movementProviders    []entity.MovementProvider
	movementRefResolver  domain.RefResolver
}

// BaseDocumentHandlerConfig configures the document handler.
type BaseDocumentHandlerConfig[T any, CreateDTO any, UpdateDTO any] struct {
	Service           domain.DocumentService[T]
	EntityName        string
	MapCreateDTO      func(dto CreateDTO) T
	MapUpdateDTO      func(dto UpdateDTO, existing T) T
	MapToDTO          func(entity T) any
	IsPostImmediately func(dto CreateDTO) bool

	// ResolveRefs batch-resolves FK → display names. Returns an opaque refs bag.
	// If nil, no resolution is performed. Called before FLS masking and DTO mapping.
	ResolveRefs func(ctx context.Context, entities ...T) (any, error)

	// MapToDTOWithRefs is an enhanced mapper that receives the resolved refs bag.
	// Used instead of MapToDTO when ResolveRefs is configured.
	MapToDTOWithRefs func(entity T, refs any) any

	// MovementProviders allow the handler to resolve cross-register movements
	MovementProviders   []entity.MovementProvider
	MovementRefResolver domain.RefResolver
}

// NewBaseDocumentHandler creates a new base document handler.
func NewBaseDocumentHandler[T any, CreateDTO any, UpdateDTO any](
	base *BaseHandler,
	cfg BaseDocumentHandlerConfig[T, CreateDTO, UpdateDTO],
) *BaseDocumentHandler[T, CreateDTO, UpdateDTO] {
	return &BaseDocumentHandler[T, CreateDTO, UpdateDTO]{
		BaseHandler:       base,
		service:           cfg.Service,
		entityName:        cfg.EntityName,
		mapCreateDTO:      cfg.MapCreateDTO,
		mapUpdateDTO:      cfg.MapUpdateDTO,
		mapToDTO:          cfg.MapToDTO,
		isPostImmediately: cfg.IsPostImmediately,
		resolveRefs:       cfg.ResolveRefs,
		mapToDTOWithRefs:  cfg.MapToDTOWithRefs,
		movementProviders:   cfg.MovementProviders,
		movementRefResolver: cfg.MovementRefResolver,
	}
}

// toDTO maps entity to DTO using the appropriate mapper.
// If refs is non-nil and mapToDTOWithRefs is configured, uses the enhanced mapper.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) toDTO(entity T, refs any) any {
	if h.mapToDTOWithRefs != nil && refs != nil {
		return h.mapToDTOWithRefs(entity, refs)
	}
	return h.mapToDTO(entity)
}

// applyFLSRead applies field-level security masking for read operations.
// Masks restricted fields on the domain entity before DTO mapping.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) applyFLSRead(c *gin.Context, entity any) {
	policy := security.GetFieldPolicy(c.Request.Context(), h.entityName, "read")
	if policy == nil {
		return
	}
	security.MaskForRead(entity, policy)
}

// Get handles GET /{entity}/:id
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Get(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, err = h.resolveRefs(ctx, doc)
		if err != nil {
			h.Error(c, err)
			return
		}
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	c.JSON(http.StatusOK, h.toDTO(doc, refs))
}

// GetMovements fetches movements for this document across all configured MovementProviders.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) GetMovements(c *gin.Context) {
	if len(h.movementProviders) == 0 {
		c.JSON(http.StatusOK, gin.H{"movements": []entity.DocumentMovement{}, "count": 0})
		return
	}

	ctx := c.Request.Context()
	docIDStr := c.Param("id")
	docID, err := id.Parse(docIDStr)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid document ID"))
		return
	}

	var allMovements []entity.DocumentMovement

	// Extract movements from every configured provider
	for _, provider := range h.movementProviders {
		moves, err := provider.GetDocumentMovements(ctx, docID)
		if err != nil {
			h.Error(c, err)
			return
		}
		allMovements = append(allMovements, moves...)
	}

	// Batch-resolve ref-type fields to human-readable names
	if h.movementRefResolver != nil {
		enrichMovementRefs(ctx, allMovements, h.movementRefResolver)
	}

	c.JSON(http.StatusOK, gin.H{
		"movements": allMovements,
		"count":     len(allMovements),
	})
}

// Create handles POST /{entity}
// Supports postImmediately flag in DTO (requires DTO to implement interface or check manually?)
// The original handler checked `req.PostImmediately`. This is tricky for generic handler.
// We can define an interface for CreateRequest or check via type assertion/reflection, OR
// pass a function to extract this flag?
// Or we can assume CreateDTO is struct and we can't easily access field without reflection/interface.
// Let's make it simple: BaseDocumentHandler Create just calls Create.
// If we need PostImmediately behavior, we might need a specific Hook or Config?
// Note: GoodsReceipt and GoodsIssue BOTH have PostImmediately.
// Let's add a `IsPostImmediately(CreateDTO) bool` function to config?
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateDTO
	if !h.BindJSON(c, &req) {
		return
	}

	// In Database-per-Tenant, no tenantID needed (isolation is physical)
	doc := h.mapCreateDTO(req)

	// Set created_by/updated_by is done in specific handler usually?
	// Or we can expect service to handle it from context strings?
	// Existing handlers set it explicitly:
	// if userID := h.GetUserID(c); userID != "" { doc.CreatedBy = userID ... }
	// We should probably add a Hook for "OnBeforeCreate"?
	// Or we can set it if T has setters?
	// T is `any`. Accessing fields is hard.
	// Maybe `MapCreateDTO` should handle setting UserID?
	// `MapCreateDTO` signature: `func(dto CreateDTO, tenantID string) T`. It doesn't get UserID.
	// We might need to change `MapCreateDTO` to accept context or UserID?
	// Or `BaseDocumentHandler` creates the doc, then we have a `Enrich(doc, c)` hook?

	// For now, let's stick to the core logic.
	// PostImmediately logic is also specific.
	// Maybe `Create` in BaseDocumentHandler is too ambitious to fully generalize without more hooks?
	// Let's try to keep it simple. If Create logic differs, we can override it in struct embedding.
	// BUT the goal IS to generalize Create.

	// Let's add `SetUserID(T, string)` to config?
	// And `IsPostImmediately(CreateDTO) bool` to config.

	if h.isPostImmediately != nil && h.isPostImmediately(req) {
		if err := h.service.PostAndSave(ctx, doc); err != nil {
			h.Error(c, err)
			return
		}
	} else {
		if err := h.service.Create(ctx, doc); err != nil {
			h.Error(c, err)
			return
		}
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// Update handles PUT /{entity}/:id
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Update(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req UpdateDTO
	if !h.BindJSON(c, &req) {
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	doc = h.mapUpdateDTO(req, doc)

	// UserID updating?

	if err := h.service.Update(ctx, doc); err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Delete handles DELETE /{entity}/:id
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Delete(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// Post handles POST /{entity}/:id/post
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Post(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Post(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	// Return updated document
	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Unpost handles POST /{entity}/:id/unpost
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Unpost(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Unpost(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	// Return updated document
	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// SetDeletionMark handles POST /{entity}/:id/deletion-mark
// Sets or clears the deletion mark. If the document is posted and we're marking it for deletion,
// the service will unpost it first (1C-style behavior: unpost + mark in one transaction).
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) SetDeletionMark(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.SetDeletionMarkRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := h.service.SetDeletionMark(ctx, docID, req.Marked); err != nil {
		h.Error(c, err)
		return
	}

	// Return updated document
	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// List handles GET /{entity} — list with filtering and pagination.
// Uses the universal filter engine via ParseListFilter.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) List(c *gin.Context) {
	ctx := c.Request.Context()

	filter, err := h.ParseListFilter(c, "-date")
	if err != nil {
		h.Error(c, err)
		return
	}

	result, err := h.service.List(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references for all items in batch (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, err = h.resolveRefs(ctx, result.Items...)
		if err != nil {
			h.Error(c, err)
			return
		}
	}

	// Map entities to DTOs (with FLS masking)
	items := make([]any, len(result.Items))
	for i, item := range result.Items {
		h.applyFLSRead(c, item)
		items[i] = h.toDTO(item, refs)
	}

	c.JSON(http.StatusOK, dto.CursorListResponse{
		Items:       items,
		NextCursor:  result.NextCursor,
		PrevCursor:  result.PrevCursor,
		HasMore:     result.HasMore,
		HasPrev:     result.HasPrev,
		TargetIndex: result.TargetIndex,
		TotalCount:  result.TotalCount,
	})
}

// ── Batch Operations ────────────────────────────────────────────────────

// batchActionRequest is the DTO for batch document operations.
type batchActionRequest struct {
	IDs    []string `json:"ids" binding:"required,min=1,max=500"`
	Action string   `json:"action" binding:"required,oneof=post unpost setDeletionMark clearDeletionMark"`
}

// batchActionResult describes the outcome for a single document in a batch.
type batchActionResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// batchActionResponse is the response for batch operations.
type batchActionResponse struct {
	Results []batchActionResult `json:"results"`
	Total   int                 `json:"total"`
	Success int                 `json:"success"`
	Failed  int                 `json:"failed"`
}

// BatchAction handles POST /{entity}/batch-action
//
// Processes each document independently (partial mode):
//   - One failure does not roll back others
//   - Returns per-item results for the client to display
//   - Permission checks are performed per-action inside the service layer
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) BatchAction(c *gin.Context) {
	ctx := c.Request.Context()

	var req batchActionRequest
	if !h.BindJSON(c, &req) {
		return
	}

	results := make([]batchActionResult, 0, len(req.IDs))
	successCount := 0

	for _, rawID := range req.IDs {
		docID, err := id.Parse(rawID)
		if err != nil {
			results = append(results, batchActionResult{ID: rawID, Error: "invalid id format"})
			continue
		}

		switch req.Action {
		case "post":
			err = h.service.Post(ctx, docID)
		case "unpost":
			err = h.service.Unpost(ctx, docID)
		case "setDeletionMark":
			err = h.service.SetDeletionMark(ctx, docID, true)
		case "clearDeletionMark":
			err = h.service.SetDeletionMark(ctx, docID, false)
		}

		r := batchActionResult{ID: rawID, Success: err == nil}
		if err != nil {
			r.Error = err.Error()
		} else {
			successCount++
		}
		results = append(results, r)
	}

	c.JSON(http.StatusOK, batchActionResponse{
		Results: results,
		Total:   len(results),
		Success: successCount,
		Failed:  len(results) - successCount,
	})
}

