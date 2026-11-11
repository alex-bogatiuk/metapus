package context

import "context"

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
