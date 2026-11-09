package handlers

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/infrastructure/cache"
	"metapus/internal/infrastructure/storage/postgres"
)

// CustomFieldHandler provides CRUD for sys_custom_field_schemas.
// Changes auto-propagate via PostgreSQL LISTEN/NOTIFY → SchemaCache invalidation.
type CustomFieldHandler struct {
	*BaseHandler
	repo  *postgres.CustomFieldRepo
	cache *cache.SchemaCache // optional — nil disables merged metadata
}

// NewCustomFieldHandler creates a new handler.
func NewCustomFieldHandler(base *BaseHandler, repo *postgres.CustomFieldRepo, schemaCache *cache.SchemaCache) *CustomFieldHandler {
	return &CustomFieldHandler{
		BaseHandler: base,
		repo:        repo,
		cache:       schemaCache,
	}
}

// --- DTOs ---

// CreateCustomFieldRequest is the request body for creating a custom field.
type CreateCustomFieldRequest struct {
	EntityType      string         `json:"entityType" binding:"required"`
	FieldName       string         `json:"fieldName" binding:"required"`
	FieldType       string         `json:"fieldType" binding:"required"`
	DisplayName     string         `json:"displayName" binding:"required"`
	Description     string         `json:"description"`
	IsRequired      bool           `json:"isRequired"`
	IsIndexed       bool           `json:"isIndexed"`
	DefaultValue    any            `json:"defaultValue"`
	ValidationRules map[string]any `json:"validationRules"`
	ReferenceType   string         `json:"referenceType"`
	EnumValues      []string       `json:"enumValues"`
	SortOrder       int            `json:"sortOrder"`
}

// UpdateCustomFieldRequest is the request body for updating a custom field.
type UpdateCustomFieldRequest struct {
	DisplayName     *string        `json:"displayName"`
	Description     *string        `json:"description"`
	IsRequired      *bool          `json:"isRequired"`
	IsIndexed       *bool          `json:"isIndexed"`
	DefaultValue    any            `json:"defaultValue"`
	ValidationRules map[string]any `json:"validationRules"`
	EnumValues      []string       `json:"enumValues"`
	SortOrder       *int           `json:"sortOrder"`
	IsActive        *bool          `json:"isActive"`
}

// CustomFieldResponse is the response body for a custom field.
type CustomFieldResponse struct {
	ID              string         `json:"id"`
	EntityType      string         `json:"entityType"`
	FieldName       string         `json:"fieldName"`
	FieldType       string         `json:"fieldType"`
	DisplayName     string         `json:"displayName"`
	Description     string         `json:"description"`
	IsRequired      bool           `json:"isRequired"`
	IsIndexed       bool           `json:"isIndexed"`
	DefaultValue    any            `json:"defaultValue,omitempty"`
	ValidationRules map[string]any `json:"validationRules,omitempty"`
	ReferenceType   string         `json:"referenceType,omitempty"`
	EnumValues      []string       `json:"enumValues,omitempty"`
	SortOrder       int            `json:"sortOrder"`
	IsActive        bool           `json:"isActive"`
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
}

// --- Handlers ---

// List returns all custom fields, optionally filtered by entityType.
// GET /api/v1/system/custom-fields?entityType=Counterparty
func (h *CustomFieldHandler) List(c *gin.Context) {
	ctx := c.Request.Context()
	entityType := c.Query("entityType")

	fields, err := h.repo.List(ctx, entityType)
	if err != nil {
		h.HandleError(c, err)
		return
	}

	result := make([]CustomFieldResponse, 0, len(fields))
	for _, f := range fields {
		result = append(result, mapCustomFieldResponse(f))
	}
	h.OK(c, result)
}

// Get returns a single custom field by ID.
// GET /api/v1/system/custom-fields/:id
func (h *CustomFieldHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	field, err := h.repo.GetByID(ctx, id)
	if err != nil {
		h.HandleError(c, err)
		return
	}
	h.OK(c, mapCustomFieldResponse(*field))
}

// Create creates a new custom field schema.
// POST /api/v1/system/custom-fields
func (h *CustomFieldHandler) Create(c *gin.Context) {
	var req CreateCustomFieldRequest
	if !h.BindJSON(c, &req) {
		return
	}

	// Validate field type
	if !isValidFieldType(req.FieldType) {
		h.HandleError(c, apperror.NewValidation("invalid field type").
			WithDetail("fieldType", req.FieldType).
			WithDetail("allowed", "string, text, integer, decimal, boolean, date, datetime, reference, enum, json"))
		return
	}

	if req.FieldType == "reference" && req.ReferenceType == "" {
		h.HandleError(c, apperror.NewValidation("referenceType is required for reference fields"))
		return
	}
	if req.FieldType == "enum" && len(req.EnumValues) == 0 {
		h.HandleError(c, apperror.NewValidation("enumValues is required for enum fields"))
		return
	}

	ctx := c.Request.Context()
	field := &postgres.CustomFieldRecord{
		EntityType:      req.EntityType,
		FieldName:       req.FieldName,
		FieldType:       req.FieldType,
		DisplayName:     req.DisplayName,
		Description:     req.Description,
		IsRequired:      req.IsRequired,
		IsIndexed:       req.IsIndexed,
		DefaultValue:    req.DefaultValue,
		ValidationRules: req.ValidationRules,
		ReferenceType:   req.ReferenceType,
		EnumValues:      req.EnumValues,
		SortOrder:       req.SortOrder,
	}

	if err := h.repo.Create(ctx, field); err != nil {
		h.HandleError(c, err)
		return
	}

	h.Created(c, field.ID)
}

// Update updates an existing custom field schema.
// PUT /api/v1/system/custom-fields/:id
func (h *CustomFieldHandler) Update(c *gin.Context) {
	var req UpdateCustomFieldRequest
	if !h.BindJSON(c, &req) {
		return
	}

	ctx := c.Request.Context()
	id := c.Param("id")

	if err := h.repo.Update(ctx, id, &postgres.CustomFieldUpdate{
		DisplayName:     req.DisplayName,
		Description:     req.Description,
		IsRequired:      req.IsRequired,
		IsIndexed:       req.IsIndexed,
		DefaultValue:    req.DefaultValue,
		ValidationRules: req.ValidationRules,
		EnumValues:      req.EnumValues,
		SortOrder:       req.SortOrder,
		IsActive:        req.IsActive,
	}); err != nil {
		h.HandleError(c, err)
		return
	}

	h.NoContent(c)
}

// Delete deactivates a custom field (soft delete via is_active = FALSE).
// DELETE /api/v1/system/custom-fields/:id
func (h *CustomFieldHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	if err := h.repo.Deactivate(ctx, id); err != nil {
		h.HandleError(c, err)
		return
	}

	h.NoContent(c)
}

// --- Helpers ---

func isValidFieldType(ft string) bool {
	switch ft {
	case "string", "text", "integer", "decimal", "boolean",
		"date", "datetime", "reference", "enum", "json":
		return true
	}
	return false
}

func mapCustomFieldResponse(f postgres.CustomFieldRecord) CustomFieldResponse {
	return CustomFieldResponse{
		ID:              f.ID,
		EntityType:      f.EntityType,
		FieldName:       f.FieldName,
		FieldType:       f.FieldType,
		DisplayName:     f.DisplayName,
		Description:     f.Description,
		IsRequired:      f.IsRequired,
		IsIndexed:       f.IsIndexed,
		DefaultValue:    f.DefaultValue,
		ValidationRules: f.ValidationRules,
		ReferenceType:   f.ReferenceType,
		EnumValues:      f.EnumValues,
		SortOrder:       f.SortOrder,
		IsActive:        f.IsActive,
		CreatedAt:       f.CreatedAt,
		UpdatedAt:       f.UpdatedAt,
	}
}
