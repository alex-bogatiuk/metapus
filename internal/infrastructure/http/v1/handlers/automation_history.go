package handlers

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationHistoryHandler exposes automation execution history via API.
type AutomationHistoryHandler struct {
	*BaseHandler
	repo automations.HistoryRepository
}

// NewAutomationHistoryHandler creates a new handler.
func NewAutomationHistoryHandler(base *BaseHandler, repo automations.HistoryRepository) *AutomationHistoryHandler {
	return &AutomationHistoryHandler{
		BaseHandler: base,
		repo:        repo,
	}
}

// List returns filtered and paginated history entries.
func (h *AutomationHistoryHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	filter := automations.HistoryFilter{
		Limit:  50,
		Offset: 0,
	}

	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			filter.Limit = parsed
		}
	}
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			filter.Offset = parsed
		}
	}
	if ruleID := c.Query("ruleId"); ruleID != "" {
		parsed, err := id.Parse(ruleID)
		if err == nil {
			filter.RuleID = &parsed
		}
	}
	if status := c.Query("status"); status != "" {
		s := automations.HistoryStatus(status)
		filter.Status = &s
	}
	if channelID := c.Query("channelId"); channelID != "" {
		parsed, err := id.Parse(channelID)
		if err == nil {
			filter.ChannelID = &parsed
		}
	}
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = &t
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = &t
		}
	}

	entries, total, err := h.repo.List(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	if entries == nil {
		entries = []automations.HistoryEntry{}
	}

	h.OK(c, gin.H{
		"items": entries,
		"total": total,
	})
}

// Get returns a single history entry.
func (h *AutomationHistoryHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	entryID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	entry, err := h.repo.GetByID(ctx, entryID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, entry)
}

// RegisterRoutes registers the history endpoints.
func (h *AutomationHistoryHandler) RegisterRoutes(rg *gin.RouterGroup) {
	history := rg.Group("/automation-history")
	{
		history.GET("", h.List)
		history.GET("/:id", h.Get)
	}
}
