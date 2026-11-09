package security

import (
	"context"

	"metapus/internal/core/apperror"
)

// CheckRLSAccess verifies that the current DataScope allows access to the entity.
// If the entity implements RLSDimensionable, its dimensions are checked against the scope.
// Returns nil if access is allowed, apperror.Forbidden otherwise.
func CheckRLSAccess(ctx context.Context, entityName string, entity any) error {
	scope := GetDataScope(ctx)
	if scope == nil || scope.IsAdmin {
		return nil
	}
	if dimensionable, ok := entity.(RLSDimensionable); ok {
		if !scope.CanAccessRecord(entityName, dimensionable.GetRLSDimensions()) {
			return apperror.NewForbidden("access denied by row-level security")
		}
	}
	return nil
}

// CheckCELPolicy evaluates CEL policy rules for the given action and entity.
// Returns nil if no PolicyEngine is provided, no rules match, or evaluation allows the action.
func CheckCELPolicy(ctx context.Context, engine *PolicyEngine, entityName, action string, entity any) error {
	if engine == nil {
		return nil
	}
	rules := GetApplicableRules(ctx, entityName, action)
	if len(rules) == 0 {
		return nil
	}
	return engine.Evaluate(ctx, rules, action, entity)
}

// FilterByReadPolicy removes items denied by CEL read rules from the slice.
// Returns the filtered slice and the count of removed items.
// If engine is nil or no rules apply, returns the original slice unchanged.
func FilterByReadPolicy[T any](ctx context.Context, engine *PolicyEngine, entityName string, items []T) ([]T, int) {
	if engine == nil {
		return items, 0
	}
	rules := GetApplicableRules(ctx, entityName, "read")
	if len(rules) == 0 {
		return items, 0
	}
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		if engine.Evaluate(ctx, rules, "read", item) == nil {
			filtered = append(filtered, item)
		}
	}
	return filtered, len(items) - len(filtered)
}
