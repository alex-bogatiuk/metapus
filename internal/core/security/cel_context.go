package security

import "context"

// --- PolicyRules context helpers ---

type policyRulesKey struct{}

// WithPolicyRules adds policy rules to the request context.
func WithPolicyRules(ctx context.Context, rules []PolicyRule) context.Context {
	if len(rules) == 0 {
		return ctx
	}
	return context.WithValue(ctx, policyRulesKey{}, rules)
}

// GetPolicyRules returns all policy rules from context.
func GetPolicyRules(ctx context.Context) []PolicyRule {
	if v, ok := ctx.Value(policyRulesKey{}).([]PolicyRule); ok {
		return v
	}
	return nil
}

// GetApplicableRules returns rules matching the given entity name and action.
func GetApplicableRules(ctx context.Context, entityName, action string) []PolicyRule {
	all := GetPolicyRules(ctx)
	if len(all) == 0 {
		return nil
	}

	var result []PolicyRule
	for _, r := range all {
		if r.MatchesEntity(entityName) && r.MatchesAction(action) {
			result = append(result, r)
		}
	}
	return result
}
