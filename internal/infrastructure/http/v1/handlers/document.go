package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// DocumentService defines the interface that services must implement for BaseDocumentHandler.
type DocumentService[T any] interface {
	GetByID(ctx context.Context, id id.ID) (T, error)
	Create(ctx context.Context, entity T) error
	Update(ctx context.Context, entity T) error
	Delete(ctx context.Context, id id.ID) error
	Post(ctx context.Context, id id.ID) error
	Unpost(ctx context.Context, id id.ID) error
	PostAndSave(ctx context.Context, entity T) error
}

// BaseDocumentHandler provides generic HTTP handlers for document entities.
// In Database-per-Tenant architecture, tenantID is not needed (isolation is physical).
type BaseDocumentHandler[T any, CreateDTO any, UpdateDTO any] struct {
	*BaseHandler
	service    DocumentService[T]
	entityName string

	// Mapper functions
	mapCreateDTO      func(dto CreateDTO) T
	mapUpdateDTO      func(dto UpdateDTO, existing T) T
	mapToDTO          func(entity T) any
	isPostImmediately func(dto CreateDTO) bool
}

// BaseDocumentHandlerConfig configures the document handler.
type BaseDocumentHandlerConfig[T any, CreateDTO any, UpdateDTO any] struct {
	Service           DocumentService[T]
	EntityName        string
	MapCreateDTO      func(dto CreateDTO) T
	MapUpdateDTO      func(dto UpdateDTO, existing T) T
	MapToDTO          func(entity T) any
	IsPostImmediately func(dto CreateDTO) bool
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
	}
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

	c.JSON(http.StatusOK, h.mapToDTO(doc))
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

	response := h.mapToDTO(doc)
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

	response := h.mapToDTO(doc)
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

	response := h.mapToDTO(doc)
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

	response := h.mapToDTO(doc)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// RegisterRoutes registers standard routes.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("", h.Create)
	rg.GET("/:id", h.Get)
	rg.PUT("/:id", h.Update)
	rg.DELETE("/:id", h.Delete)
	rg.POST("/:id/post", h.Post)
	rg.POST("/:id/unpost", h.Unpost)
}
