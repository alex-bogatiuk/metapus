package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/userpref"
)

// UserPrefsHandler handles user preferences endpoints.
type UserPrefsHandler struct {
	*BaseHandler
	repo userpref.Repository
}

// NewUserPrefsHandler creates a new user preferences handler.
func NewUserPrefsHandler(base *BaseHandler, repo userpref.Repository) *UserPrefsHandler {
	return &UserPrefsHandler{
		BaseHandler: base,
		repo:        repo,
	}
}

// GetPreferences handles GET /me/preferences
func (h *UserPrefsHandler) GetPreferences(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := h.requireUserID(c)
	if err != nil {
		return
	}

	prefs, err := h.repo.GetOrCreate(ctx, userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, prefs)
}

// SaveInterface handles PUT /me/preferences/interface
func (h *UserPrefsHandler) SaveInterface(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := h.requireUserID(c)
	if err != nil {
		return
	}

	var prefs userpref.InterfacePrefs
	if !h.BindJSON(c, &prefs) {
		return
	}

	if err := h.repo.SaveInterface(ctx, userID, prefs); err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// SaveListFilters handles PUT /me/preferences/list-filters/:entityType
func (h *UserPrefsHandler) SaveListFilters(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := h.requireUserID(c)
	if err != nil {
		return
	}

	entityType := c.Param("entityType")
	if entityType == "" {
		h.Error(c, apperror.NewValidation("entityType is required"))
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.Error(c, apperror.NewValidation("failed to read request body"))
		return
	}

	// Validate that body is valid JSON
	if !json.Valid(body) {
		h.Error(c, apperror.NewValidation("request body must be valid JSON"))
		return
	}

	if err := h.repo.SaveListFilters(ctx, userID, entityType, json.RawMessage(body)); err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// SaveListColumns handles PUT /me/preferences/list-columns/:entityType
func (h *UserPrefsHandler) SaveListColumns(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := h.requireUserID(c)
	if err != nil {
		return
	}

	entityType := c.Param("entityType")
	if entityType == "" {
		h.Error(c, apperror.NewValidation("entityType is required"))
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.Error(c, apperror.NewValidation("failed to read request body"))
		return
	}

	if !json.Valid(body) {
		h.Error(c, apperror.NewValidation("request body must be valid JSON"))
		return
	}

	if err := h.repo.SaveListColumns(ctx, userID, entityType, json.RawMessage(body)); err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// requireUserID extracts and parses user ID from context, sending error if absent.
func (h *UserPrefsHandler) requireUserID(c *gin.Context) (id.ID, error) {
	raw := h.GetUserID(c)
	if raw == "" {
		err := apperror.NewUnauthorized("not authenticated")
		h.Error(c, err)
		return id.ID{}, err
	}

	userID, err := id.Parse(raw)
	if err != nil {
		appErr := apperror.NewValidation("invalid user id")
		h.Error(c, appErr)
		return id.ID{}, appErr
	}

	return userID, nil
}

// RegisterRoutes registers user preferences routes on the given router group.
func (h *UserPrefsHandler) RegisterRoutes(rg *gin.RouterGroup) {
	me := rg.Group("/me/preferences")
	{
		me.GET("", h.GetPreferences)
		me.PUT("/interface", h.SaveInterface)
		me.PUT("/list-filters/:entityType", h.SaveListFilters)
		me.PUT("/list-columns/:entityType", h.SaveListColumns)
	}
}
