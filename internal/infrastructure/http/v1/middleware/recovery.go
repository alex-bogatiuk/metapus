// Package middleware provides HTTP middleware components.
package middleware

import (
	"fmt"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/eventlog"
	"metapus/internal/core/tenant"
	"metapus/pkg/logger"
)

// Recovery middleware recovers from panics and returns 500 error.
// Logs stack trace but never exposes internal details to client.
// Writes system.panic event to event log if pool is available in context.
func Recovery(eventWriter eventlog.DirectWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				ctx := c.Request.Context()
				stack := string(debug.Stack())

				// Log full stack trace
				logger.Error(ctx, "panic recovered",
					"error", err,
					"stack", stack,
				)

				// Write event log (best-effort, pool may not be available)
				if eventWriter != nil {
					if pool, poolErr := tenant.GetPool(ctx); poolErr == nil {
						_ = eventWriter.WriteDirect(ctx, pool, eventlog.Event{
							Category:  eventlog.CategorySystem,
							Severity:  eventlog.SeverityCritical,
							EventType: eventlog.EventSystemPanic,
							Source:    "api",
							ClientIP:  c.ClientIP(),
							Message:   fmt.Sprintf("panic: %v", err),
							Details: map[string]any{
								"path":   c.Request.URL.Path,
								"method": c.Request.Method,
								"stack":  stack,
							},
						})
					}
				}

				_ = c.Error(
					apperror.NewInternal(fmt.Errorf("panic: %v", err)).
						WithDetail("request_id", c.GetString("request_id")),
				)
				c.Abort()
			}
		}()
		c.Next()
	}
}
