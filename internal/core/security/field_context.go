package security

import "context"

// --- FieldPolicy context helpers ---

type fieldPoliciesKey struct{}

// WithFieldPolicies adds field policies map to context.
// Key format: "entity_name:action" (e.g. "crypto_invoice:read").
func WithFieldPolicies(ctx context.Context, policies map[string]*FieldPolicy) context.Context {
	// A nil policy set explicitly clears the previous principal's policies.
	if policies == nil && GetFieldPolicies(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, fieldPoliciesKey{}, policies)
}

// GetFieldPolicies returns all field policies from context.
func GetFieldPolicies(ctx context.Context) map[string]*FieldPolicy {
	if v, ok := ctx.Value(fieldPoliciesKey{}).(map[string]*FieldPolicy); ok {
		return v
	}
	return nil
}

// GetFieldPolicy returns the FieldPolicy for a specific entity and action.
// Returns nil if no policy is defined (no FLS restrictions).
func GetFieldPolicy(ctx context.Context, entityName, action string) *FieldPolicy {
	policies := GetFieldPolicies(ctx)
	if policies == nil {
		return nil
	}
	return policies[entityName+":"+action]
}
