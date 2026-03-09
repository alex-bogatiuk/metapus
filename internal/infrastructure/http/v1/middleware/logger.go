package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	
	"metapus/pkg/logger"
)

// Logger middleware logs HTTP requests with timing and status.
func Logger(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		
		// Process request
		c.Next()
		
		// Calculate latency
		latency := time.Since(start)
		status := c.Writer.Status()
		
		// Build log entry
		log.WithContext(c.Request.Context()).Infow("http request",
			"method", c.Request.Method,
			"path", path,
			"query", query,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
			"error", c.Errors.ByType(gin.ErrorTypePrivate).String(),
		)
	}
}
