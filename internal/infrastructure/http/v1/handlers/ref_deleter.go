package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/domain"
)

// MarkedObjectsHandler handles "Delete Marked Objects" processing.
type MarkedObjectsHandler struct {
	processor domain.MarkedObjectsProcessor
}

// NewMarkedObjectsHandler creates a new MarkedObjectsHandler.
func NewMarkedObjectsHandler(processor domain.MarkedObjectsProcessor) *MarkedObjectsHandler {
	return &MarkedObjectsHandler{processor: processor}
}

// List handles GET /system/marked-objects — list all deletion-marked entities.
func (h *MarkedObjectsHandler) List(c *gin.Context) {
	results, err := h.processor.ListMarkedObjects(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if results == nil {
		results = []domain.MarkedObject{}
	}

	c.JSON(http.StatusOK, gin.H{"items": results, "total": len(results)})
}

// deleteMarkedRequest wraps array of items to delete.
type deleteMarkedRequest struct {
	Items []domain.DeleteMarkedRequest `json:"items" binding:"required,min=1"`
}

// Delete handles POST /system/marked-objects/delete — physically delete entities.
func (h *MarkedObjectsHandler) Delete(c *gin.Context) {
	var req deleteMarkedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation("invalid request: " + err.Error()))
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
