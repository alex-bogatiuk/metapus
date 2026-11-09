package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/infrastructure/cache"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/metadata"
)

type MetadataHandler struct {
	registry    *metadata.Registry
	schemaCache *cache.SchemaCache
}

func NewMetadataHandler(registry *metadata.Registry, schemaCache *cache.SchemaCache) *MetadataHandler {
	return &MetadataHandler{
		registry:    registry,
		schemaCache: schemaCache,
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

// ListEntitiesSummary returns a lightweight list of all registered entities.
// Includes summary-safe contract data — no field definitions.
// GET /api/v1/meta/entities
func (h *MetadataHandler) ListEntitiesSummary(c *gin.Context) {
	entities := h.registry.List()
	result := make([]dto.EntitySummaryResponse, 0, len(entities))
	for _, e := range entities {
		result = append(result, dto.EntitySummaryResponse{
			Key:          e.Key,
			Name:         e.Name,
			Type:         e.Type,
			Presentation: e.Presentation,
			RoutePrefix:  e.RoutePrefix,
			Capabilities: e.Capabilities,
			Actions:      e.Actions,
		})
	}
	c.JSON(http.StatusOK, result)
}

// GetEntity returns the full metadata for a specific entity.
// GET /api/v1/meta/:name
func (h *MetadataHandler) GetEntity(c *gin.Context) {
	name := c.Param("name")
	if def, ok := h.registry.Get(name); ok {
		// Merge custom fields from cache before returning
		h.mergeCustomFields(&def)
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
	// Merge custom fields so mock data includes them
	h.mergeCustomFields(&def)
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
	// Merge custom fields so they appear in filters
	h.mergeCustomFields(&def)
	c.JSON(http.StatusOK, def.ToFilterMeta(h.registry))
}

// mergeCustomFields translates the cache snapshot without coupling metadata to storage.
func (h *MetadataHandler) mergeCustomFields(def *metadata.EntityDef) {
	if h.schemaCache == nil {
		return
	}
	fields := make([]metadata.FieldDef, 0)
	for _, cf := range h.schemaCache.GetCustomFields(def.Key) {
		fieldType := metadata.TypeString
		switch cf.FieldType {
		case "integer":
			fieldType = metadata.TypeInteger
		case "decimal":
			fieldType = metadata.TypeNumber
		case "boolean":
			fieldType = metadata.TypeBoolean
		case "date", "datetime":
			fieldType = metadata.TypeDate
		case "reference":
			fieldType = metadata.TypeReference
		case "enum":
			fieldType = metadata.TypeEnum
		}
		fields = append(fields, metadata.FieldDef{Name: "attributes." + cf.FieldName, Label: cf.DisplayName, Type: fieldType, Required: cf.IsRequired, Options: append([]string(nil), cf.EnumValues...), ReferenceType: cf.ReferenceType})
	}
	def.MergeCustomFields(fields)
}
