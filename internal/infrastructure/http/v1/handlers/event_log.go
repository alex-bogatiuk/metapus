package handlers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/eventlog"
	"metapus/internal/core/id"
	"metapus/internal/domain/cursor"
	"metapus/internal/infrastructure/http/v1/dto"
)

// parseDateParam parses a date query parameter accepting both RFC3339 and plain date (YYYY-MM-DD).
// When endOfDay is true and only a date is provided, it returns 23:59:59.999999999 of that day.
func parseDateParam(raw string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return t, nil
}

// EventLogHandler provides HTTP handlers for the system event log.
type EventLogHandler struct {
	BaseHandler
	reader eventlog.Reader

	// In-memory stats cache (TTL 30s)
	statsMu    sync.RWMutex
	statsCache *eventlog.Stats
	statsAt    time.Time
}

// NewEventLogHandler creates a new event log handler.
func NewEventLogHandler(reader eventlog.Reader) *EventLogHandler {
	return &EventLogHandler{reader: reader}
}

// List handles GET /system/event-log — list events with cursor pagination.
func (h *EventLogHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	f := eventlog.DefaultFilter()

	// Parse filter params
	if v := c.Query("category"); v != "" {
		for _, cat := range strings.Split(v, ",") {
			f.Categories = append(f.Categories, eventlog.Category(strings.TrimSpace(cat)))
		}
	}
	if v := c.Query("severity"); v != "" {
		for _, sev := range strings.Split(v, ",") {
			f.Severities = append(f.Severities, eventlog.Severity(strings.TrimSpace(sev)))
		}
	}
	if v := c.Query("eventType"); v != "" {
		f.EventType = v
	}
	if v := c.Query("userId"); v != "" {
		f.UserID = v
	}
	if v := c.Query("entityType"); v != "" {
		f.EntityType = v
	}
	if v := c.Query("entityId"); v != "" {
		parsed, err := id.Parse(v)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid entityId"))
			return
		}
		f.EntityID = &parsed
	}
	if v := c.Query("entityNumber"); v != "" {
		f.EntityNumber = v
	}
	if v := c.Query("source"); v != "" {
		f.Source = v
	}
	if v := c.Query("search"); v != "" {
		f.Search = v
	}
	if v := c.Query("traceId"); v != "" {
		f.TraceID = v
	}
	if v := c.Query("dateFrom"); v != "" {
		t, err := parseDateParam(v, false)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid dateFrom format, expected YYYY-MM-DD or RFC3339"))
			return
		}
		f.DateFrom = &t
	}
	if v := c.Query("dateTo"); v != "" {
		t, err := parseDateParam(v, true)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid dateTo format, expected YYYY-MM-DD or RFC3339"))
			return
		}
		f.DateTo = &t
	}
	if v := c.Query("orderBy"); v != "" {
		f.OrderBy = v
	}

	// Parse cursor pagination
	var cursorReq *cursor.Request
	if after := c.Query("after"); after != "" {
		cursorReq = &cursor.Request{Direction: cursor.DirAfter, Token: after}
	} else if before := c.Query("before"); before != "" {
		cursorReq = &cursor.Request{Direction: cursor.DirBefore, Token: before}
	}

	result, err := h.reader.List(ctx, f, cursorReq)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.CursorListResponse{
		Items:      dto.FromEventLogEntries(result.Items),
		NextCursor: result.NextCursor,
		PrevCursor: result.PrevCursor,
		HasMore:    result.HasMore,
		HasPrev:    result.HasPrev,
		TotalCount: result.TotalCount,
	})
}

// Get handles GET /system/event-log/:id — get single event details.
func (h *EventLogHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	eventID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid event id"))
		return
	}

	event, err := h.reader.GetByID(ctx, eventID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromEventLogEntry(event))
}

// Trace handles GET /system/event-log/trace/:traceId — get trace chain.
func (h *EventLogHandler) Trace(c *gin.Context) {
	ctx := c.Request.Context()

	traceID := c.Param("traceId")
	if traceID == "" {
		h.Error(c, apperror.NewValidation("traceId is required"))
		return
	}

	events, err := h.reader.GetByTraceID(ctx, traceID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": dto.FromEventLogEntries(events)})
}

// Stats handles GET /system/event-log/stats — aggregated statistics.
// Cached in memory for 30 seconds to avoid expensive COUNT queries on every request.
func (h *EventLogHandler) Stats(c *gin.Context) {
	ctx := c.Request.Context()

	var sf eventlog.StatsFilter
	if v := c.Query("dateFrom"); v != "" {
		t, err := parseDateParam(v, false)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid dateFrom format"))
			return
		}
		sf.DateFrom = &t
	}
	if v := c.Query("dateTo"); v != "" {
		t, err := parseDateParam(v, true)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid dateTo format"))
			return
		}
		sf.DateTo = &t
	}

	// Check cache with read lock (only for unfiltered stats)
	if sf.DateFrom == nil && sf.DateTo == nil {
		h.statsMu.RLock()
		if h.statsCache != nil && time.Since(h.statsAt) < 30*time.Second {
			cached := *h.statsCache
			h.statsMu.RUnlock()
			c.JSON(http.StatusOK, dto.FromEventLogStats(cached))
			return
		}
		h.statsMu.RUnlock()
	}

	stats, err := h.reader.GetStats(ctx, sf)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Update cache for unfiltered queries (write lock)
	if sf.DateFrom == nil && sf.DateTo == nil {
		h.statsMu.Lock()
		h.statsCache = &stats
		h.statsAt = time.Now()
		h.statsMu.Unlock()
	}

	c.JSON(http.StatusOK, dto.FromEventLogStats(stats))
}
