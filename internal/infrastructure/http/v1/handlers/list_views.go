package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/listview"
	"metapus/internal/infrastructure/http/v1/dto"
)

// ListViewHandler handles list view CRUD endpoints.
type ListViewHandler struct {
	*BaseHandler
	service *listview.Service
}

// NewListViewHandler creates a new list view handler.
func NewListViewHandler(base *BaseHandler, service *listview.Service) *ListViewHandler {
	return &ListViewHandler{
		BaseHandler: base,
		service:     service,
	}
}

// GetList handles GET /me/list-views/:entityType
func (h *ListViewHandler) GetList(c *gin.Context) {
	ctx := c.Request.Context()

	entityType := c.Param("entityType")
	if entityType == "" {
		h.Error(c, apperror.NewValidation("entityType is required"))
		return
	}

	list, err := h.service.GetList(ctx, entityType)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.MapListViewListResponse(list))
}

// Create handles POST /me/list-views
func (h *ListViewHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.CreateListViewRequest
	if !h.BindJSON(c, &req) {
		return
	}

	view := &listview.ListView{
		ID:         uuid.New(),
		EntityType: req.EntityType,
		Name:       req.Name,
		Visibility: req.Visibility,
		IsDefault:  req.IsDefault,
		Config:     req.Config,
	}

	if err := h.service.Create(ctx, view); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, dto.MapListViewResponse(view))
}

// Update handles PUT /me/list-views/:id
func (h *ListViewHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid UUID"))
		return
	}

	var req dto.UpdateListViewRequest
	if !h.BindJSON(c, &req) {
		return
	}

	view := &listview.ListView{
		ID:         id,
		Name:       req.Name,
		Visibility: req.Visibility,
		IsDefault:  req.IsDefault,
		Config:     req.Config,
		Version:    req.Version,
	}

	if err := h.service.Update(ctx, view); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.MapListViewResponse(view))
}

// Delete handles DELETE /me/list-views/:id
func (h *ListViewHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid UUID"))
		return
	}

	if err := h.service.Delete(ctx, id); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// SetDefault handles PUT /me/list-views/:id/default
func (h *ListViewHandler) SetDefault(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid UUID"))
		return
	}

	if err := h.service.SetDefault(ctx, id); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// RegisterListViewRoutes registers list view routes on the given router group.
func RegisterListViewRoutes(rg *gin.RouterGroup, handler *ListViewHandler) {
	views := rg.Group("/me/list-views")
	{
		views.GET("/:entityType", handler.GetList)
		views.POST("", handler.Create)
		views.PUT("/:id", handler.Update)
		views.DELETE("/:id", handler.Delete)
		views.PUT("/:id/default", handler.SetDefault)
	}
}
