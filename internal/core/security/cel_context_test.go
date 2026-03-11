package security

import (
	"context"
	"testing"
)

// --- stub PolicyRule for tests ---

type stubRule struct {
	id         string
	expression string
	effect     string
	priority   int
	enabled    bool
	entity     string
	actions    []string
}

func (s *stubRule) GetID() string         { return s.id }
func (s *stubRule) GetExpression() string { return s.expression }
func (s *stubRule) GetEffect() string     { return s.effect }
func (s *stubRule) GetPriority() int      { return s.priority }
func (s *stubRule) GetEnabled() bool      { return s.enabled }
func (s *stubRule) MatchesAction(action string) bool {
	for _, a := range s.actions {
		if a == "*" || a == action {
			return true
		}
	}
	return false
}
func (s *stubRule) MatchesEntity(entityName string) bool {
	return s.entity == "*" || s.entity == entityName
}

// --- tests ---

func TestWithPolicyRules_Empty(t *testing.T) {
	ctx := context.Background()
	ctx2 := WithPolicyRules(ctx, nil)
	if ctx2 != ctx {
		t.Fatal("expected same context when rules is nil")
	}
	ctx3 := WithPolicyRules(ctx, []PolicyRule{})
	if ctx3 != ctx {
		t.Fatal("expected same context when rules is empty slice")
	}
}

func TestGetPolicyRules_None(t *testing.T) {
	ctx := context.Background()
	rules := GetPolicyRules(ctx)
	if rules != nil {
		t.Fatal("expected nil when no rules in context")
	}
}

func TestWithPolicyRules_AndGetPolicyRules(t *testing.T) {
	r1 := &stubRule{id: "r1", expression: "true", effect: "deny", priority: 10, enabled: true, entity: "order", actions: []string{"create"}}
	r2 := &stubRule{id: "r2", expression: "false", effect: "allow", priority: 5, enabled: true, entity: "*", actions: []string{"*"}}

	ctx := WithPolicyRules(context.Background(), []PolicyRule{r1, r2})
	rules := GetPolicyRules(ctx)

	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].GetID() != "r1" {
		t.Fatalf("expected r1 first, got %s", rules[0].GetID())
	}
}

func TestGetApplicableRules_NoRulesInContext(t *testing.T) {
	ctx := context.Background()
	rules := GetApplicableRules(ctx, "order", "create")
	if rules != nil {
		t.Fatal("expected nil when no rules in context")
	}
}

func TestGetApplicableRules_MatchesEntityAndAction(t *testing.T) {
	r1 := &stubRule{id: "r1", entity: "order", actions: []string{"create"}, enabled: true, expression: "true", effect: "deny", priority: 10}
	r2 := &stubRule{id: "r2", entity: "order", actions: []string{"update"}, enabled: true, expression: "true", effect: "deny", priority: 5}
	r3 := &stubRule{id: "r3", entity: "goods_receipt", actions: []string{"create"}, enabled: true, expression: "true", effect: "deny", priority: 1}

	ctx := WithPolicyRules(context.Background(), []PolicyRule{r1, r2, r3})

	applicable := GetApplicableRules(ctx, "order", "create")
	if len(applicable) != 1 {
		t.Fatalf("expected 1 applicable rule, got %d", len(applicable))
	}
	if applicable[0].GetID() != "r1" {
		t.Fatalf("expected r1, got %s", applicable[0].GetID())
	}
}

func TestGetApplicableRules_Wildcard(t *testing.T) {
	r1 := &stubRule{id: "r1", entity: "*", actions: []string{"*"}, enabled: true, expression: "true", effect: "deny", priority: 10}

	ctx := WithPolicyRules(context.Background(), []PolicyRule{r1})

	applicable := GetApplicableRules(ctx, "any_entity", "any_action")
	if len(applicable) != 1 {
		t.Fatalf("expected 1 wildcard rule, got %d", len(applicable))
	}
}

func TestGetApplicableRules_NoMatch(t *testing.T) {
	r1 := &stubRule{id: "r1", entity: "order", actions: []string{"create"}, enabled: true, expression: "true", effect: "deny", priority: 10}

	ctx := WithPolicyRules(context.Background(), []PolicyRule{r1})

	applicable := GetApplicableRules(ctx, "goods_receipt", "update")
	if len(applicable) != 0 {
		t.Fatalf("expected 0 applicable rules, got %d", len(applicable))
	}
}
