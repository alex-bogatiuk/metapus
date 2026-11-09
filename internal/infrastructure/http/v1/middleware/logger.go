package middleware

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/domain/eventlog"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/pkg/logger"
)

// slowRequestThreshold defines latency above which a request is logged as api.slow_request.
const _slowRequestThreshold = 3 * time.Second

// Logger middleware logs HTTP requests with timing and status.
// Also writes event log entries for 5xx errors and slow requests.
func Logger(log *logger.Logger, eventWriter DirectEventWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		queryKeys := safeQueryKeys(c.Request.URL.Query())

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
			"query_keys", queryKeys,
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
		pool, poolErr := tenantdb.GetPool(ctx)
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
					"method":     c.Request.Method,
					"path":       path,
					"query_keys": queryKeys,
					"status":     status,
					"error":      c.Errors.ByType(gin.ErrorTypePrivate).String(),
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
					"query_keys": queryKeys,
					"status":     status,
					"latency_ms": latency.Milliseconds(),
				},
			})
		}
	}
}

func safeQueryKeys(values map[string][]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.Join(keys, ",")
}
