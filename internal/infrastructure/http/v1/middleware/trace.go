package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	platformtelemetry "github.com/alex-bogatiuk/metapus/internal/platform/telemetry"
)

const (
	HeaderRequestID = "X-Request-ID"
	HeaderTraceID   = "X-Trace-ID"
)

// Trace middleware adds request tracing context.
// Extracts or generates trace IDs for distributed tracing.
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		parent := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		spanCtx, span := otel.Tracer("github.com/alex-bogatiuk/metapus/http").Start(parent, c.Request.Method,
			oteltrace.WithSpanKind(oteltrace.SpanKindServer))
		defer span.End()
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
		spanID := uuid.New().String()[:16]
		if span.SpanContext().IsValid() {
			traceID = span.SpanContext().TraceID().String()
			spanID = span.SpanContext().SpanID().String()
		}

		// Create trace context
		trace := &appctx.TraceContext{
			TraceID:   traceID,
			SpanID:    spanID,
			RequestID: requestID,
		}

		// Add to context
		ctx := appctx.WithTrace(spanCtx, trace)
		ctx = platformtelemetry.WithBusinessContext(ctx, requestID, requestID)
		c.Request = c.Request.WithContext(ctx)

		// Store in gin context for easy access
		c.Set("trace_id", traceID)
		c.Set("request_id", requestID)

		// Add to response headers
		c.Header(HeaderRequestID, requestID)
		c.Header(HeaderTraceID, traceID)

		c.Next()
		span.SetName(c.Request.Method + " " + c.FullPath())
		span.SetAttributes(attribute.String("http.route", c.FullPath()),
			attribute.Int("http.response.status_code", c.Writer.Status()))
	}
}
