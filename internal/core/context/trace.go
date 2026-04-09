package context

import (
	"context"

	"github.com/google/uuid"
)

// TraceContext contains request tracing information.
type TraceContext struct {
	TraceID   string
	SpanID    string
	RequestID string
}

type traceContextKey struct{}

// WithTrace adds TraceContext to context.
func WithTrace(ctx context.Context, trace *TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, trace)
}

// GetTrace returns TraceContext from context.
func GetTrace(ctx context.Context) *TraceContext {
	if v, ok := ctx.Value(traceContextKey{}).(*TraceContext); ok {
		return v
	}
	return nil
}

// GetTraceID returns trace ID from context or generates new one.
func GetTraceID(ctx context.Context) string {
	if t := GetTrace(ctx); t != nil {
		return t.TraceID
	}
	return uuid.New().String()
}

// GetRequestID returns request ID from context or empty string.
func GetRequestID(ctx context.Context) string {
	if t := GetTrace(ctx); t != nil {
		return t.RequestID
	}
	return ""
}

// NewTraceContext creates a new TraceContext with generated IDs.
func NewTraceContext() *TraceContext {
	return &TraceContext{
		TraceID:   uuid.New().String(),
		SpanID:    uuid.New().String()[:16],
		RequestID: uuid.New().String(),
	}
}
