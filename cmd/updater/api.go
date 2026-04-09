// cmd/updater/api.go
package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/pkg/logger"
)

// APIHandler exposes the updater REST API.
type APIHandler struct {
	orch *Orchestrator
	log  *logger.Logger
}

// NewAPIHandler creates REST handlers for the updater.
func NewAPIHandler(orch *Orchestrator, log *logger.Logger) *APIHandler {
	return &APIHandler{orch: orch, log: log}
}

// RegisterRoutes mounts all updater API routes.
func (h *APIHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/updater")
	{
		g.GET("/status", h.Status)
		g.GET("/available", h.Available)
		g.POST("/start", h.Start)
		g.POST("/rollback", h.Rollback)
		g.POST("/reset", h.Reset)
		g.GET("/log", h.LogSSE)
	}
}

// StatusResponse is the full state snapshot returned by GET /updater/status.
type StatusResponse struct {
	Phase          Phase      `json:"phase"`
	PhaseDetail    string     `json:"phaseDetail,omitempty"`
	TargetImage    string     `json:"targetImage,omitempty"`
	TargetTag      string     `json:"targetTag,omitempty"`
	OldContainerID string     `json:"oldContainerId,omitempty"`
	NewContainerID string     `json:"newContainerId,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	LastError      string     `json:"lastError,omitempty"`
	PullCurrent    int64      `json:"pullCurrent,omitempty"`
	PullTotal      int64      `json:"pullTotal,omitempty"`
	LogLength      int        `json:"logLength"`
}

// Status returns the current updater state.
// GET /updater/status
func (h *APIHandler) Status(c *gin.Context) {
	st := h.orch.state.Get()
	resp := StatusResponse{
		Phase:          st.Phase,
		PhaseDetail:    st.PhaseDetail,
		TargetImage:    st.TargetImage,
		TargetTag:      st.TargetTag,
		OldContainerID: truncateID(st.OldContainerID),
		NewContainerID: truncateID(st.NewContainerID),
		LastError:      st.LastError,
		PullCurrent:    st.PullCurrent,
		PullTotal:      st.PullTotal,
		LogLength:      len(st.Log),
	}
	if !st.StartedAt.IsZero() {
		resp.StartedAt = &st.StartedAt
	}
	if !st.CompletedAt.IsZero() {
		resp.CompletedAt = &st.CompletedAt
	}
	c.JSON(http.StatusOK, resp)
}

// Available returns current server version info.
// GET /updater/available
func (h *APIHandler) Available(c *gin.Context) {
	info, err := h.orch.CheckAvailable(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

// StartRequest is the body for POST /updater/start.
type StartRequest struct {
	Tag string `json:"tag" binding:"required"`
}

// Start begins an update to the specified image tag.
// POST /updater/start
// Body: { "tag": "v1.5.0" }
func (h *APIHandler) Start(c *gin.Context) {
	var req StartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag is required"})
		return
	}

	if err := h.orch.Start(c.Request.Context(), req.Tag); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "update started",
		"target":  fmt.Sprintf("%s:%s", h.orch.cfg.RegistryImage, req.Tag),
		"phase":   PhaseChecking,
	})
}

// Rollback restores the previous container.
// POST /updater/rollback
func (h *APIHandler) Rollback(c *gin.Context) {
	if err := h.orch.Rollback(c.Request.Context()); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "rollback started",
		"phase":   PhaseRollback,
	})
}

// LogSSE streams the update log via Server-Sent Events.
// GET /updater/log
func (h *APIHandler) LogSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// Send existing log entries
	st := h.orch.state.Get()
	for _, entry := range st.Log {
		_, _ = fmt.Fprintf(c.Writer, "data: %s | %s\n\n", entry.Time.Format("15:04:05"), entry.Message)
	}
	c.Writer.Flush()

	// Stream new entries
	lastSeen := len(st.Log)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			st = h.orch.state.Get()
			if len(st.Log) > lastSeen {
				for _, entry := range st.Log[lastSeen:] {
					_, _ = fmt.Fprintf(c.Writer, "data: %s | %s\n\n", entry.Time.Format("15:04:05"), entry.Message)
				}
				c.Writer.Flush()
				lastSeen = len(st.Log)
			}
		}
	}
}

// Reset resets the state machine back to idle (only from failed/done).
func (h *APIHandler) Reset(c *gin.Context) {
	st := h.orch.state.Get()
	if st.Phase != PhaseFailed && st.Phase != PhaseDone {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("cannot reset from phase: %s", st.Phase)})
		return
	}

	if err := h.orch.state.Update(func(s *UpdateState) {
		*s = UpdateState{Phase: PhaseIdle}
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.log.Info("state reset to idle", "from", st.Phase)
	c.JSON(http.StatusOK, gin.H{"message": "state reset to idle"})
}

func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
