package security_profile

import (
	"context"
	"testing"
)

func TestPolicyRule_Validate_Valid(t *testing.T) {
	rule := &PolicyRule{
		Name:       "deny large amounts",
		EntityName: "goods_receipt",
		Actions:    []string{"create", "update"},
		Expression: "doc.amount > 1000000",
		Effect:     EffectDeny,
		Priority:   10,
		Enabled:    true,
	}

	if err := rule.Validate(context.Background()); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestPolicyRule_Validate_MissingName(t *testing.T) {
	rule := &PolicyRule{
		EntityName: "goods_receipt",
		Actions:    []string{"create"},
		Expression: "true",
		Effect:     EffectDeny,
	}
	if err := rule.Validate(context.Background()); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestPolicyRule_Validate_MissingEntityName(t *testing.T) {
	rule := &PolicyRule{
		Name:       "rule",
		Actions:    []string{"create"},
		Expression: "true",
		Effect:     EffectDeny,
	}
	if err := rule.Validate(context.Background()); err == nil {
		t.Fatal("expected error for missing entity_name")
	}
}

func TestPolicyRule_Validate_MissingExpression(t *testing.T) {
	rule := &PolicyRule{
		Name:       "rule",
		EntityName: "goods_receipt",
		Actions:    []string{"create"},
		Effect:     EffectDeny,
	}
	if err := rule.Validate(context.Background()); err == nil {
		t.Fatal("expected error for missing expression")
	}
}

func TestPolicyRule_Validate_EmptyActions(t *testing.T) {
	rule := &PolicyRule{
		Name:       "rule",
		EntityName: "goods_receipt",
		Actions:    []string{},
		Expression: "true",
		Effect:     EffectDeny,
	}
	if err := rule.Validate(context.Background()); err == nil {
		t.Fatal("expected error for empty actions")
	}
}

func TestPolicyRule_Validate_InvalidEffect(t *testing.T) {
	rule := &PolicyRule{
		Name:       "rule",
		EntityName: "goods_receipt",
		Actions:    []string{"create"},
		Expression: "true",
		Effect:     "permit",
	}
	if err := rule.Validate(context.Background()); err == nil {
		t.Fatal("expected error for invalid effect")
	}
}

func TestPolicyRule_Validate_AllowEffect(t *testing.T) {
	rule := &PolicyRule{
		Name:       "rule",
		EntityName: "goods_receipt",
		Actions:    []string{"read"},
		Expression: "user.isAdmin == true",
		Effect:     EffectAllow,
	}
	if err := rule.Validate(context.Background()); err != nil {
		t.Fatalf("allow effect should be valid: %v", err)
	}
}

func TestPolicyRule_MatchesAction(t *testing.T) {
	rule := &PolicyRule{Actions: []string{"create", "update"}}

	cases := []struct {
		action  string
		matches bool
	}{
		{"create", true},
		{"update", true},
		{"delete", false},
		{"read", false},
	}

	for _, tc := range cases {
		got := rule.MatchesAction(tc.action)
		if got != tc.matches {
			t.Errorf("MatchesAction(%q) = %v, want %v", tc.action, got, tc.matches)
		}
	}
}

func TestPolicyRule_MatchesAction_Wildcard(t *testing.T) {
	rule := &PolicyRule{Actions: []string{"*"}}
	for _, action := range []string{"create", "read", "update", "delete", "post", "unpost"} {
		if !rule.MatchesAction(action) {
			t.Errorf("wildcard should match action %q", action)
		}
	}
}

func TestPolicyRule_MatchesEntity(t *testing.T) {
	rule := &PolicyRule{EntityName: "goods_receipt"}

	if !rule.MatchesEntity("goods_receipt") {
		t.Error("should match exact entity")
	}
	if rule.MatchesEntity("goods_issue") {
		t.Error("should not match different entity")
	}
}

func TestPolicyRule_MatchesEntity_Wildcard(t *testing.T) {
	rule := &PolicyRule{EntityName: "*"}

	for _, entity := range []string{"goods_receipt", "goods_issue", "counterparty", "order"} {
		if !rule.MatchesEntity(entity) {
			t.Errorf("wildcard should match entity %q", entity)
		}
	}
}

func TestPolicyRule_InterfaceMethods(t *testing.T) {
	rule := &PolicyRule{
		Expression: "doc.status == 'draft'",
		Effect:     EffectDeny,
		Priority:   42,
		Enabled:    true,
	}

	if rule.GetExpression() != "doc.status == 'draft'" {
		t.Error("GetExpression mismatch")
	}
	if rule.GetEffect() != EffectDeny {
		t.Error("GetEffect mismatch")
	}
	if rule.GetPriority() != 42 {
		t.Error("GetPriority mismatch")
	}
	if !rule.GetEnabled() {
		t.Error("GetEnabled should be true")
	}
}
