package handlers

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/workerjob"
	"metapus/internal/infrastructure/http/v1/dto"
)

// WorkerJobHandler serves the /system/worker-jobs API for background job observability.
type WorkerJobHandler struct {
	repo workerjob.Repository

	// stats cache — prevents high-frequency COUNT on every dashboard load
	mu            sync.Mutex
	cachedStats   *dto.WorkerJobStatsResponse
	statsCachedAt time.Time
	statsTTL      time.Duration
}

// NewWorkerJobHandler creates a handler.
func NewWorkerJobHandler(repo workerjob.Repository) *WorkerJobHandler {
	return &WorkerJobHandler{
		repo:     repo,
		statsTTL: 30 * time.Second,
	}
}

// RegisterRoutes wires all worker-job routes under the provided group.
func (h *WorkerJobHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/worker-jobs", h.List)
	rg.GET("/worker-jobs/stats", h.Stats)
}

// List godoc
//
//	@Summary     List worker job runs
//	@Description Returns cursor-paginated background task execution records
//	@Tags        system
//	@Produce     json
//	@Param       jobName     query  string false "Filter by job name (e.g. outbox.relay)"
//	@Param       jobCategory query  string false "Filter by category (crypto|outbox|cleanup|automation)"
//	@Param       status      query  string false "Filter by status (running|success|error|skipped)"
//	@Param       dateFrom    query  string false "ISO8601 start timestamp"
//	@Param       dateTo      query  string false "ISO8601 end timestamp"
//	@Param       after       query  string false "Cursor token for next page"
//	@Param       limit       query  int    false "Page size (default 50)"
//	@Success     200  {object} dto.WorkerJobListResponse
//	@Router      /system/worker-jobs [get]
func (h *WorkerJobHandler) List(c *gin.Context) {
	f := workerjob.Filter{
		JobName:     c.Query("jobName"),
		JobCategory: c.Query("jobCategory"),
		Status:      c.Query("status"),
		After:       c.Query("after"),
	}

	if s := c.Query("dateFrom"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.DateFrom = &t
		}
	}
	if s := c.Query("dateTo"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.DateTo = &t
		}
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		var l int
		if _, err := parseInt(limitStr, &l); err == nil && l > 0 && l <= 200 {
			f.Limit = l
		}
	}

	result, err := h.repo.List(c.Request.Context(), f)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.WorkerJobListResponse{
		Items:      dto.MapWorkerJobs(result.Items),
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
		TotalCount: result.TotalCount,
	})
}

// Stats godoc
//
//	@Summary     Worker job KPI stats
//	@Description Returns aggregate counts for the last 24h (cached 30s)
//	@Tags        system
//	@Produce     json
//	@Success     200  {object} dto.WorkerJobStatsResponse
//	@Router      /system/worker-jobs/stats [get]
func (h *WorkerJobHandler) Stats(c *gin.Context) {
	h.mu.Lock()
	if h.cachedStats != nil && time.Since(h.statsCachedAt) < h.statsTTL {
		stats := h.cachedStats
		h.mu.Unlock()
		c.JSON(http.StatusOK, stats)
		return
	}
	h.mu.Unlock()

	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)

	stats, err := h.repo.GetStats(c.Request.Context(), from, now)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	resp := &dto.WorkerJobStatsResponse{
		Total:       stats.Total,
		Success:     stats.Success,
		Error:       stats.Error,
		AvgDuration: stats.AvgDuration,
	}

	h.mu.Lock()
	h.cachedStats = resp
	h.statsCachedAt = now
	h.mu.Unlock()

	c.JSON(http.StatusOK, resp)
}

// parseInt is a helper to parse a query int string.
func parseInt(s string, out *int) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	*out = n
	return n, nil
}
