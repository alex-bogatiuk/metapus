// Package security provides security-related utilities including user context management.
package security

import "context"

type userIDKey struct{}

// WithUserID adds user ID to context.
// Used by middleware to propagate authenticated user through request chain.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// GetUserID retrieves user ID from context.
// Returns empty string if not found.
//
// Usage in domain layer:
//
//	userID := security.GetUserID(ctx)
//	if userID != "" {
//	    entity.CreatedBy = userID
//	}
func GetUserID(ctx context.Context) string {
	if uid, ok := ctx.Value(userIDKey{}).(string); ok {
		return uid
	}
	return ""
}
