package handlers

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationChannelHandler handles API endpoints for automation channels.
type AutomationChannelHandler struct {
	*BaseHandler
	repo automations.ChannelRepository
}

// NewAutomationChannelHandler creates a new handler.
func NewAutomationChannelHandler(base *BaseHandler, repo automations.ChannelRepository) *AutomationChannelHandler {
	return &AutomationChannelHandler{
		BaseHandler: base,
		repo:        repo,
	}
}

// List returns all channels, optionally filtered by accountId.
func (h *AutomationChannelHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	var accountID *id.ID
	if aid := c.Query("accountId"); aid != "" {
		parsed, err := id.Parse(aid)
		if err == nil {
			accountID = &parsed
		}
	}

	channels, err := h.repo.List(ctx, accountID)
	if err != nil {
		h.Error(c, err)
		return
	}

	if channels == nil {
		channels = []automations.Channel{}
	}

	h.OK(c, channels)
}

// Get returns a single channel.
func (h *AutomationChannelHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	channelID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	channel, err := h.repo.GetByID(ctx, channelID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, channel)
}

// Create handles the creation of a new channel.
func (h *AutomationChannelHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req automations.CreateChannelRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := req.Validate(ctx); err != nil {
		h.Error(c, err)
		return
	}

	channel, err := h.repo.Create(ctx, req)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Created(c, channel.ID.String())
}

// Update modifies an existing channel.
func (h *AutomationChannelHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()

	channelID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	var req automations.UpdateChannelRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := req.Validate(ctx); err != nil {
		h.Error(c, err)
		return
	}

	channel, err := h.repo.Update(ctx, channelID, req)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, channel)
}

// Delete removes a channel.
func (h *AutomationChannelHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	channelID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	if err := h.repo.Delete(ctx, channelID); err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// RegisterRoutes registers the handlers to the Gin router group.
func (h *AutomationChannelHandler) RegisterRoutes(rg *gin.RouterGroup) {
	channels := rg.Group("/automation-channels")
	{
		channels.GET("", h.List)
		channels.POST("", h.Create)
		channels.GET("/:id", h.Get)
		channels.PUT("/:id", h.Update)
		channels.DELETE("/:id", h.Delete)
	}
}
