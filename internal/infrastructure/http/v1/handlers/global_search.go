package handlers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/domain/search"
)

// GlobalSearchHandler handles GET /api/v1/search.
type GlobalSearchHandler struct {
	service *search.Service
}

// NewGlobalSearchHandler creates a new global search handler.
func NewGlobalSearchHandler(service *search.Service) *GlobalSearchHandler {
	return &GlobalSearchHandler{service: service}
}

// Search handles global data search requests.
// GET /api/v1/search?q=<query>&limit=<limitPerEntity>
func (h *GlobalSearchHandler) Search(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))

	limitPerEntity := 5
	if v := c.Query("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limitPerEntity = parsed
		}
	}

	result, err := h.service.Search(c.Request.Context(), query, limitPerEntity)
	if err != nil {
		c.Error(err)
		c.Abort()
		return
	}

	c.JSON(200, result)
}
