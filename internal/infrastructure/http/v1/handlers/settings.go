package handlers

import (
	"io"
	"metapus/internal/infrastructure/http/v1/dto"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/json"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/middleware"
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

// UpdateSection handles PATCH /settings/:section — updates a single section with optimistic locking.
func (h *SettingsHandler) UpdateSection(c *gin.Context) {
	ctx := c.Request.Context()

	section := c.Param("section")
	if section == "" {
		h.Error(c, apperror.NewValidation("section parameter is required"))
		return
	}

	const maxSettingsBody = 1 << 20 // 1 MiB
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSettingsBody+1))
	if err != nil {
		h.Error(c, apperror.NewValidation("failed to read request body"))
		return
	}
	if len(body) > maxSettingsBody {
		appErr := apperror.NewValidation("request body too large")
		appErr.HTTPStatus = http.StatusRequestEntityTooLarge
		h.Error(c, appErr)
		return
	}

	var req dto.UpdateSettingsSectionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.Error(c, apperror.NewInvalidRequest(err))
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
func (h *SettingsHandler) RegisterRoutes(rg *gin.RouterGroup, updateGuards ...gin.HandlerFunc) {
	sg := rg.Group("/settings")
	sg.Use(middleware.RequirePermissionOrSystemAdmin(auth.PermissionSystemAdmin))
	{
		sg.GET("", h.Get)
		sg.PATCH("/:section", appendRouteGuards(updateGuards, h.UpdateSection)...)
	}
}
