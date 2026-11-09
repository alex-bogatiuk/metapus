package checkout

import "context"

type contextKey struct{}

func WithReference(ctx context.Context, ref *Reference) context.Context {
	return context.WithValue(ctx, contextKey{}, ref)
}

func ReferenceFromContext(ctx context.Context) *Reference {
	if v, ok := ctx.Value(contextKey{}).(*Reference); ok {
		return v
	}
	return nil
}
