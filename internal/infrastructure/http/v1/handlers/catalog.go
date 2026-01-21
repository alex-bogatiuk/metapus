// Package handlers provides HTTP request handlers.
package handlers

import (
	"net/http"

	"encoding/json"
	"github.com/gin-gonic/gin"
	domainFilter "metapus/internal/domain/filter"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/v1/dto"
)

// CatalogHandler provides generic HTTP handlers for catalog entities.
// In Database-per-Tenant architecture, tenantID is not needed (isolation is physical).
type CatalogHandler[T entity.Validatable, CreateDTO any, UpdateDTO any] struct {
	*BaseHandler
	service    *domain.CatalogService[T]
	entityName string

	// Mapper functions
	mapCreateDTO func(dto CreateDTO) T
	mapUpdateDTO func(dto UpdateDTO, existing T) T
	mapToDTO     func(entity T) any
}

// CatalogHandlerConfig configures the catalog handler.
type CatalogHandlerConfig[T entity.Validatable, CreateDTO any, UpdateDTO any] struct {
	Service      *domain.CatalogService[T]
	EntityName   string
	MapCreateDTO func(dto CreateDTO) T
	MapUpdateDTO func(dto UpdateDTO, existing T) T
	MapToDTO     func(entity T) any
}

// NewCatalogHandler creates a new catalog handler.
func NewCatalogHandler[T entity.Validatable, CreateDTO any, UpdateDTO any](
	base *BaseHandler,
	cfg CatalogHandlerConfig[T, CreateDTO, UpdateDTO],
) *CatalogHandler[T, CreateDTO, UpdateDTO] {
	return &CatalogHandler[T, CreateDTO, UpdateDTO]{
		BaseHandler:  base,
		service:      cfg.Service,
		entityName:   cfg.EntityName,
		mapCreateDTO: cfg.MapCreateDTO,
		mapUpdateDTO: cfg.MapUpdateDTO,
		mapToDTO:     cfg.MapToDTO,
	}
}

// List handles GET /{entity} - list with filtering and pagination.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) List(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse filter from query params
	filter := domain.DefaultListFilter()
	filter.Search = c.Query("search")
	filter.Limit = h.ParseIntQuery(c, "limit", 50)
	filter.Offset = h.ParseIntQuery(c, "offset", 0)
	filter.OrderBy = c.DefaultQuery("orderBy", "name")
	filter.IncludeDeleted = c.Query("includeDeleted") == "true"

	if parentID := c.Query("parentId"); parentID != "" {
		filter.ParentID = &parentID
	}

	if isFolder := c.Query("isFolder"); isFolder != "" {
		val := isFolder == "true"
		filter.IsFolder = &val
	}

	// Парсинг JSON-фильтра
	filterJson := c.Query("filter")
	if filterJson != "" {
		var advFilters []domainFilter.Item // используем alias для пакета filter
		if err := json.Unmarshal([]byte(filterJson), &advFilters); err != nil {
			h.Error(c, apperror.NewValidation("invalid filter format (json expected)"))
			return
		}
		filter.AdvancedFilters = advFilters
	}

	result, err := h.service.List(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Map entities to DTOs
	items := make([]any, len(result.Items))
	for i, item := range result.Items {
		items[i] = h.mapToDTO(item)
	}

	c.JSON(http.StatusOK, dto.ListResponse{
		Items:      items,
		TotalCount: result.TotalCount,
		Limit:      result.Limit,
		Offset:     result.Offset,
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

	c.JSON(http.StatusOK, h.mapToDTO(entity))
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

	// Complete idempotency with created entity
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", h.mapToDTO(entity))

	c.JSON(http.StatusCreated, h.mapToDTO(entity))
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

	// Complete idempotency
	h.CompleteIdempotency(c, http.StatusOK, "application/json", h.mapToDTO(updated))

	c.JSON(http.StatusOK, h.mapToDTO(updated))
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

	// Вызываем метод сервиса
	if err := h.service.SetDeletionMark(ctx, entityID, req.Marked); err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, "deletion mark updated")
}

// GetTree handles GET /{entity}/tree - get hierarchical structure.
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

	// Map to DTOs
	dtos := make([]any, len(items))
	for i, item := range items {
		dtos[i] = h.mapToDTO(item)
	}

	c.JSON(http.StatusOK, gin.H{"items": dtos})
}
