package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/metadata"
)

type MetadataHandler struct {
	registry *metadata.Registry
}

func NewMetadataHandler(registry *metadata.Registry) *MetadataHandler {
	return &MetadataHandler{
		registry: registry,
	}
}

// ListEntities returns a list of all registered entities (summarized).
// GET /api/v1/meta
func (h *MetadataHandler) ListEntities(c *gin.Context) {
	// We might want to return a simplified list (names/types/labels) only,
	// but for now returning full definitions is fine for MVP.
	entities := h.registry.List()
	c.JSON(http.StatusOK, entities)
}

// GetEntity returns the full metadata for a specific entity.
// GET /api/v1/meta/:name
func (h *MetadataHandler) GetEntity(c *gin.Context) {
	name := c.Param("name")
	if def, ok := h.registry.Get(name); ok {
		c.JSON(http.StatusOK, def)
	} else {
		c.Status(http.StatusNotFound)
	}
}
