package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/eventlog"
	"metapus/internal/core/tenant"
	"metapus/pkg/logger"
)

// slowRequestThreshold defines latency above which a request is logged as api.slow_request.
const _slowRequestThreshold = 3 * time.Second

// Logger middleware logs HTTP requests with timing and status.
// Also writes event log entries for 5xx errors and slow requests.
func Logger(log *logger.Logger, eventWriter eventlog.DirectWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)
		status := c.Writer.Status()

		ctx := c.Request.Context()

		// Build log entry
		log.WithContext(ctx).Infow("http request",
			"method", c.Request.Method,
			"path", path,
			"query", query,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
			"error", c.Errors.ByType(gin.ErrorTypePrivate).String(),
		)

		// Write event log for errors and slow requests (best-effort)
		if eventWriter == nil {
			return
		}
		pool, poolErr := tenant.GetPool(ctx)
		if poolErr != nil {
			return
		}

		durationMs := int(latency.Milliseconds())

		if status >= 500 {
			_ = eventWriter.WriteDirect(ctx, pool, eventlog.Event{
				Category:   eventlog.CategoryAPI,
				Severity:   eventlog.SeverityError,
				EventType:  eventlog.EventAPIError500,
				Source:     "api",
				ClientIP:   c.ClientIP(),
				Message:    fmt.Sprintf("%s %s → %d", c.Request.Method, path, status),
				DurationMs: &durationMs,
				Details: map[string]any{
					"method": c.Request.Method,
					"path":   path,
					"query":  query,
					"status": status,
					"error":  c.Errors.ByType(gin.ErrorTypePrivate).String(),
				},
			})
		} else if latency >= _slowRequestThreshold {
			_ = eventWriter.WriteDirect(ctx, pool, eventlog.Event{
				Category:   eventlog.CategoryAPI,
				Severity:   eventlog.SeverityWarning,
				EventType:  eventlog.EventAPISlowRequest,
				Source:     "api",
				ClientIP:   c.ClientIP(),
				Message:    fmt.Sprintf("%s %s → %dms (slow)", c.Request.Method, path, latency.Milliseconds()),
				DurationMs: &durationMs,
				Details: map[string]any{
					"method":     c.Request.Method,
					"path":       path,
					"query":      query,
					"status":     status,
					"latency_ms": latency.Milliseconds(),
				},
			})
		}
	}
}
