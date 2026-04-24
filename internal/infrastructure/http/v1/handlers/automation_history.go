package handlers

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/automations"
	"metapus/internal/infrastructure/storage/postgres"
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

// parseHistoryFilter extracts common filter params from query string.
func (h *AutomationHistoryHandler) parseHistoryFilter(c *gin.Context) automations.HistoryFilter {
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

	return filter
}

// List returns filtered and paginated history entries.
func (h *AutomationHistoryHandler) List(c *gin.Context) {
	ctx := c.Request.Context()
	filter := h.parseHistoryFilter(c)

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

// Stats returns aggregated counts grouped by status.
func (h *AutomationHistoryHandler) Stats(c *gin.Context) {
	ctx := c.Request.Context()
	filter := h.parseHistoryFilter(c)

	stats, err := h.repo.CountByStatus(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, stats)
}

// BatchReplay enqueues replay tasks for all error entries matching the filter.
func (h *AutomationHistoryHandler) BatchReplay(c *gin.Context) {
	ctx := c.Request.Context()
	filter := h.parseHistoryFilter(c)

	// Force status=error for safety — only failed entries can be replayed in batch
	errorStatus := automations.HistoryError
	filter.Status = &errorStatus

	ids, err := h.repo.ListIDsByStatus(ctx, filter, 200)
	if err != nil {
		h.Error(c, err)
		return
	}

	if len(ids) == 0 {
		h.OK(c, gin.H{"queued": 0})
		return
	}

	pub := postgres.NewOutboxPublisher()
	queued := 0
	for _, entryID := range ids {
		err := pub.Publish(ctx, domain.DomainEvent{
			AggregateType: "automation_history",
			AggregateID:   entryID,
			EventType:     "replay",
			Payload:       map[string]any{"history_id": entryID.String()},
		})
		if err != nil {
			continue // best-effort: skip individual failures
		}
		queued++
	}

	h.OK(c, gin.H{"queued": queued})
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

// Replay publishes an event to the outbox to trigger a retry of a failed message.
func (h *AutomationHistoryHandler) Replay(c *gin.Context) {
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

	if entry.RenderedPayload == nil || *entry.RenderedPayload == "" {
		h.Error(c, apperror.NewValidation("cannot replay empty payload"))
		return
	}

	// Write to outbox
	pub := postgres.NewOutboxPublisher()
	err = pub.Publish(ctx, domain.DomainEvent{
		AggregateType: "automation_history",
		AggregateID:   entryID,
		EventType:     "replay",
		Payload:       map[string]any{"history_id": entryID.String()},
	})
	if err != nil {
		h.Error(c, apperror.NewInternal(fmt.Errorf("failed to enqueue replay task: %w", err)))
		return
	}

	h.OK(c, gin.H{"id": entryID.String(), "status": "queued"})
}

// RegisterRoutes registers the history endpoints.
func (h *AutomationHistoryHandler) RegisterRoutes(rg *gin.RouterGroup) {
	history := rg.Group("/automation-history")
	{
		history.GET("", h.List)
		history.GET("/stats", h.Stats)
		history.GET("/:id", h.Get)
		history.POST("/:id/replay", h.Replay)
		history.POST("/batch-replay", h.BatchReplay)
	}
}
