package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/settings"
)

// SettingsHandler handles system settings endpoints.
type SettingsHandler struct {
	*BaseHandler
	repo settings.Repository
}

// NewSettingsHandler creates a new settings handler.
func NewSettingsHandler(base *BaseHandler, repo settings.Repository) *SettingsHandler {
	return &SettingsHandler{
		BaseHandler: base,
		repo:        repo,
	}
}

// Get handles GET /settings — returns the full settings object.
func (h *SettingsHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	s, err := h.repo.Get(ctx)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, s)
}

// updateSectionRequest is the request body for PATCH /settings/:section.
type updateSectionRequest struct {
	Data    json.RawMessage `json:"data"    binding:"required"`
	Version int             `json:"version" binding:"required"`
}

// UpdateSection handles PATCH /settings/:section — updates a single section with optimistic locking.
func (h *SettingsHandler) UpdateSection(c *gin.Context) {
	ctx := c.Request.Context()

	section := c.Param("section")
	if section == "" {
		h.Error(c, apperror.NewValidation("section parameter is required"))
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.Error(c, apperror.NewValidation("failed to read request body"))
		return
	}

	var req updateSectionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.Error(c, apperror.NewValidation("invalid request body: "+err.Error()))
		return
	}

	if len(req.Data) == 0 {
		h.Error(c, apperror.NewValidation("data field is required"))
		return
	}

	updated, err := h.repo.UpdateSection(ctx, section, req.Data, req.Version)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, updated)
}

// RegisterRoutes registers settings routes on the given router group.
func (h *SettingsHandler) RegisterRoutes(rg *gin.RouterGroup) {
	sg := rg.Group("/settings")
	{
		sg.GET("", h.Get)
		sg.PATCH("/:section", h.UpdateSection)
	}
}
