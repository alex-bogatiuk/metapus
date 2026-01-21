// Package middleware provides HTTP middleware components.
package middleware

import (
	"fmt"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	
	"metapus/internal/core/apperror"
	"metapus/pkg/logger"
)

// Recovery middleware recovers from panics and returns 500 error.
// Logs stack trace but never exposes internal details to client.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Log full stack trace
				logger.Error(c.Request.Context(), "panic recovered",
					"error", err,
					"stack", string(debug.Stack()),
				)

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
