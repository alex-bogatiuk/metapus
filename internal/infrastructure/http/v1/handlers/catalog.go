// Package handlers provides HTTP request handlers.
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

// CatalogHandler provides generic HTTP handlers for catalog entities.
// In Database-per-Tenant architecture, tenantID is not needed (isolation is physical).
type CatalogHandler[T entity.CatalogEntity, CreateDTO any, UpdateDTO any] struct {
	*BaseHandler
	service    *domain.CatalogService[T]
	entityName string

	// Mapper functions
	mapCreateDTO func(dto CreateDTO) T
	mapUpdateDTO func(dto UpdateDTO, existing T) T
	mapToDTO     func(entity T) any

	// ResolveRefs batch-resolves FK → display names. Returns an opaque refs bag.
	// If nil, no resolution is performed (same pattern as BaseDocumentHandler).
	resolveRefs      func(ctx context.Context, entities ...T) (any, error)
	mapToDTOWithRefs func(entity T, refs any) any
}

// CatalogHandlerConfig configures the catalog handler.
type CatalogHandlerConfig[T entity.CatalogEntity, CreateDTO any, UpdateDTO any] struct {
	Service      *domain.CatalogService[T]
	EntityName   string
	MapCreateDTO func(dto CreateDTO) T
	MapUpdateDTO func(dto UpdateDTO, existing T) T
	MapToDTO     func(entity T) any

	// ResolveRefs batch-resolves FK → display names. Returns an opaque refs bag.
	// If nil, no resolution is performed. Called before FLS masking and DTO mapping.
	ResolveRefs func(ctx context.Context, entities ...T) (any, error)

	// MapToDTOWithRefs is an enhanced mapper that receives the resolved refs bag.
	// Used instead of MapToDTO when ResolveRefs is configured.
	MapToDTOWithRefs func(entity T, refs any) any
}

// NewCatalogHandler creates a new catalog handler.
func NewCatalogHandler[T entity.CatalogEntity, CreateDTO any, UpdateDTO any](
	base *BaseHandler,
	cfg CatalogHandlerConfig[T, CreateDTO, UpdateDTO],
) *CatalogHandler[T, CreateDTO, UpdateDTO] {
	return &CatalogHandler[T, CreateDTO, UpdateDTO]{
		BaseHandler:      base,
		service:          cfg.Service,
		entityName:       cfg.EntityName,
		mapCreateDTO:     cfg.MapCreateDTO,
		mapUpdateDTO:     cfg.MapUpdateDTO,
		mapToDTO:         cfg.MapToDTO,
		resolveRefs:      cfg.ResolveRefs,
		mapToDTOWithRefs: cfg.MapToDTOWithRefs,
	}
}

// toDTO maps entity to DTO using the appropriate mapper.
// If refs is non-nil and mapToDTOWithRefs is configured, uses the enhanced mapper.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) toDTO(entity T, refs any) any {
	if h.mapToDTOWithRefs != nil && refs != nil {
		return h.mapToDTOWithRefs(entity, refs)
	}
	return h.mapToDTO(entity)
}

// List handles GET /{entity} - list with filtering and pagination.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) List(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse common filter params via shared helper
	filter, err := h.ParseListFilter(c, "name")
	if err != nil {
		h.Error(c, err)
		return
	}

	// Catalog-specific: hierarchical filters
	if parentIDStr := c.Query("parentId"); parentIDStr != "" {
		parsed, err := id.Parse(parentIDStr)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid parentId format"))
			return
		}
		filter.ParentID = &parsed
	}

	if isFolder := c.Query("isFolder"); isFolder != "" {
		val := isFolder == "true"
		filter.IsFolder = &val
	}

	result, err := h.service.List(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references for all items in batch (if configured)
	var refs any
	if h.resolveRefs != nil {
		var err2 error
		refs, err2 = h.resolveRefs(ctx, result.Items...)
		if err2 != nil {
			h.Error(c, err2)
			return
		}
	}

	// Map entities to DTOs (with FLS masking)
	policy := security.GetFieldPolicy(ctx, h.entityName, "read")
	items := make([]any, len(result.Items))
	for i, item := range result.Items {
		if policy != nil {
			security.MaskForRead(item, policy)
		}
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

// Get handles GET /{entity}/:id - get single entity.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) Get(c *gin.Context) {
	ctx := c.Request.Context()

	entityID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	entity, err := h.service.GetByID(ctx, entityID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, err = h.resolveRefs(ctx, entity)
		if err != nil {
			h.Error(c, err)
			return
		}
	}

	// FLS: mask restricted fields before DTO mapping
	if policy := security.GetFieldPolicy(ctx, h.entityName, "read"); policy != nil {
		security.MaskForRead(entity, policy)
	}

	c.JSON(http.StatusOK, h.toDTO(entity, refs))
}

// Create handles POST /{entity} - create new entity.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateDTO
	if !h.BindJSON(c, &req) {
		return
	}

	// Map DTO to domain entity
	// In Database-per-Tenant, no tenantID needed (isolation is physical)
	entity := h.mapCreateDTO(req)

	if err := h.service.Create(ctx, entity); err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, entity)
	}

	response := h.toDTO(entity, refs)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// Update handles PUT /{entity}/:id - update existing entity.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) Update(c *gin.Context) {
	ctx := c.Request.Context()

	entityID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req UpdateDTO
	if !h.BindJSON(c, &req) {
		return
	}

	// Get existing entity
	existing, err := h.service.GetByID(ctx, entityID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Map update DTO onto existing entity
	updated := h.mapUpdateDTO(req, existing)

	if err := h.service.Update(ctx, updated); err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, updated)
	}

	response := h.toDTO(updated, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Delete handles DELETE /{entity}/:id - soft delete entity.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	entityID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Delete(ctx, entityID); err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// SetDeletionMark handles POST /{entity}/:id/deletion-mark
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) SetDeletionMark(c *gin.Context) {
	ctx := c.Request.Context()

	idStr := c.Param("id")
	entityID, err := id.Parse(idStr)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.SetDeletionMarkRequest
	if !h.BindJSON(c, &req) {
		return
	}

	// Call service method
	if err := h.service.SetDeletionMark(ctx, entityID, req.Marked); err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, "deletion mark updated")
}

// GetTree handles GET /{entity}/tree - get hierarchical structure.
// Returns a nested tree with "children" arrays for frontend consumption.
// For flat catalogs, returns 400 Bad Request (handled by CatalogService).
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) GetTree(c *gin.Context) {
	ctx := c.Request.Context()

	var rootID *id.ID
	if rootStr := c.Query("rootId"); rootStr != "" {
		parsed, err := id.Parse(rootStr)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid rootId format"))
			return
		}
		rootID = &parsed
	}

	items, err := h.service.GetTree(ctx, rootID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references for all tree items in batch (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, items...)
	}

	// Build TreeNodes: map DTOs and extract hierarchy info from entities
	nodes := make([]*TreeNode, len(items))
	for i, item := range items {
		node := &TreeNode{
			Data: h.toDTO(item, refs),
		}
		// Extract hierarchy fields via ParentAccessor interface
		if accessor, ok := any(item).(interface {
			GetID() id.ID
			GetParentID() *id.ID
			GetIsFolder() bool
		}); ok {
			node.ID = accessor.GetID()
			node.ParentID = accessor.GetParentID()
			node.IsFolder = accessor.GetIsFolder()
		}
		nodes[i] = node
	}

	tree := BuildTreeFromNodes(nodes)
	c.JSON(http.StatusOK, gin.H{"items": tree})
}
