package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/search"
)

// EntityPreviewHandler handles GET /api/v1/search/preview.
type EntityPreviewHandler struct {
	service *search.Service
}

// NewEntityPreviewHandler creates a new entity preview handler.
func NewEntityPreviewHandler(service *search.Service) *EntityPreviewHandler {
	return &EntityPreviewHandler{service: service}
}

// Preview handles entity preview requests.
// GET /api/v1/search/preview?entityType=catalog&entityKey=counterparty&id=<uuid>
func (h *EntityPreviewHandler) Preview(c *gin.Context) {
	entityType := strings.TrimSpace(c.Query("entityType"))
	entityKey := strings.TrimSpace(c.Query("entityKey"))
	entityID := strings.TrimSpace(c.Query("id"))

	if entityType == "" || entityKey == "" || entityID == "" {
		_ = c.Error(apperror.NewValidation("entityType, entityKey, and id are required"))
		c.Abort()
		return
	}

	result, err := h.service.Preview(c.Request.Context(), entityType, entityKey, entityID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(200, result)
}
