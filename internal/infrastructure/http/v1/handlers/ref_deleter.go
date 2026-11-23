package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
)

// MarkedObjectsHandler handles "Delete Marked Objects" processing.
type MarkedObjectsHandler struct {
	processor domain.MarkedObjectsProcessor
}

// NewMarkedObjectsHandler creates a new MarkedObjectsHandler.
func NewMarkedObjectsHandler(processor domain.MarkedObjectsProcessor) *MarkedObjectsHandler {
	return &MarkedObjectsHandler{processor: processor}
}

// List handles GET /system/marked-objects — the deletion plan for every
// marked object.
func (h *MarkedObjectsHandler) List(c *gin.Context) {
	plan, err := h.processor.PlanDeletion(c.Request.Context(), nil)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, plan)
}

// Plan handles POST /system/marked-objects/plan — the deletion plan for the
// selected objects.
func (h *MarkedObjectsHandler) Plan(c *gin.Context) {
	var req dto.PlanMarkedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}
	selection := req.Items
	if selection == nil {
		selection = []domain.MarkedObjectRef{}
	}
	plan, err := h.processor.PlanDeletion(c.Request.Context(), selection)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, plan)
}

// Delete handles POST /system/marked-objects/delete — physically delete the
// selected objects the plan allows.
func (h *MarkedObjectsHandler) Delete(c *gin.Context) {
	var req dto.DeleteMarkedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	result, err := h.processor.DeleteMarked(c.Request.Context(), req.Items)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, result)
}
