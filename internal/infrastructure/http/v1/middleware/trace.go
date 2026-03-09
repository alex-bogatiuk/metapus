package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	
	appctx "metapus/internal/core/context"
)

const (
	HeaderRequestID  = "X-Request-ID"
	HeaderTraceID    = "X-Trace-ID"
)

// Trace middleware adds request tracing context.
// Extracts or generates trace IDs for distributed tracing.
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get or generate request ID
		requestID := c.GetHeader(HeaderRequestID)
		if requestID == "" {
			requestID = uuid.New().String()
		}
		
		// Get or generate trace ID
		traceID := c.GetHeader(HeaderTraceID)
		if traceID == "" {
			traceID = uuid.New().String()
		}
		
		// Create trace context
		trace := &appctx.TraceContext{
			TraceID:   traceID,
			SpanID:    uuid.New().String()[:16],
			RequestID: requestID,
		}
		
		// Add to context
		ctx := appctx.WithTrace(c.Request.Context(), trace)
		c.Request = c.Request.WithContext(ctx)
		
		// Store in gin context for easy access
		c.Set("trace_id", traceID)
		c.Set("request_id", requestID)
		
		// Add to response headers
		c.Header(HeaderRequestID, requestID)
		c.Header(HeaderTraceID, traceID)
		
		c.Next()
	}
}
