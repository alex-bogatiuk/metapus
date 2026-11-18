package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/metadata"
)

type MetadataHandler struct {
	registry *metadata.Registry
}

func NewMetadataHandler(registry *metadata.Registry) *MetadataHandler {
	return &MetadataHandler{registry: registry}
}

// ListEntities returns a list of all registered entities (summarized).
// GET /api/v1/meta
func (h *MetadataHandler) ListEntities(c *gin.Context) {
	// We might want to return a simplified list (names/types/labels) only,
	// but for now returning full definitions is fine for MVP.
	entities := h.registry.List()
	c.JSON(http.StatusOK, entities)
}

// ListEntitiesSummary returns a lightweight list of all registered entities.
// Includes summary-safe contract data — no field definitions.
// GET /api/v1/meta/entities
func (h *MetadataHandler) ListEntitiesSummary(c *gin.Context) {
	entities := h.registry.List()
	result := make([]dto.EntitySummaryResponse, 0, len(entities))
	for _, e := range entities {
		result = append(result, dto.EntitySummaryResponse{
			Key:            e.Key,
			Name:           e.Name,
			Type:           e.Type,
			Presentation:   e.Presentation,
			RoutePrefix:    e.RoutePrefix,
			Capabilities:   e.Capabilities,
			Actions:        e.Actions,
			Code:           e.Code,
			ReadPermission: e.ReadPermission,
		})
	}
	c.JSON(http.StatusOK, result)
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

// GetEntityMock returns a sample JSON document for a specific entity.
// Used by the CEL sandbox to auto-populate sample data.
// GET /api/v1/meta/:name/mock
func (h *MetadataHandler) GetEntityMock(c *gin.Context) {
	name := c.Param("name")
	def, ok := h.registry.Get(name)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, def.GenerateMock())
}

// GetEntityFilters returns a flat list of FilterFieldMeta for a specific entity.
// This is the contract between backend and frontend for the filter configuration dialog.
// GET /api/v1/meta/:name/filters
func (h *MetadataHandler) GetEntityFilters(c *gin.Context) {
	name := c.Param("name")
	def, ok := h.registry.Get(name)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, def.ToFilterMeta(h.registry))
}
