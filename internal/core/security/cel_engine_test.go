package security

import (
	"context"
	"testing"
	"time"
)

// --- helpers ---

func newTestEngine(t *testing.T) *PolicyEngine {
	t.Helper()
	engine, err := NewPolicyEngine()
	if err != nil {
		t.Fatalf("NewPolicyEngine: %v", err)
	}
	return engine
}

func makeRule(id, expr, effect string, priority int) *stubRule {
	return &stubRule{
		id:         id,
		expression: expr,
		effect:     effect,
		priority:   priority,
		enabled:    true,
		entity:     "order",
		actions:    []string{"create"},
	}
}

// --- Compile tests ---

func TestPolicyEngine_Compile_Valid(t *testing.T) {
	engine := newTestEngine(t)

	cases := []string{
		"true",
		"false",
		"doc.status == 'draft'",
		"doc.amount < 1000000",
		"action == 'create'",
		"user.isAdmin == true",
	}
	for _, expr := range cases {
		if err := engine.Compile(expr); err != nil {
			t.Errorf("Compile(%q) unexpected error: %v", expr, err)
		}
	}
}

func TestPolicyEngine_Compile_Invalid_Syntax(t *testing.T) {
	engine := newTestEngine(t)

	if err := engine.Compile("doc.status =="); err == nil {
		t.Error("expected error for invalid syntax")
	}
}

func TestPolicyEngine_Compile_NonBool(t *testing.T) {
	engine := newTestEngine(t)

	if err := engine.Compile("'hello'"); err == nil {
		t.Error("expected error when expression returns non-bool")
	}
	if err := engine.Compile("42"); err == nil {
		t.Error("expected error when expression returns int")
	}
}

// --- Evaluate tests ---

func TestPolicyEngine_Evaluate_NoRules(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	err := engine.Evaluate(ctx, nil, "create", map[string]any{"status": "draft"})
	if err != nil {
		t.Fatalf("expected nil when no rules, got %v", err)
	}
}

func TestPolicyEngine_Evaluate_DenyRule_Matches(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	rule := makeRule("r1", "doc.amount > 500", "deny", 10)
	entity := &celTestEntity{Amount: 1000, Status: "draft"}

	err := engine.Evaluate(ctx, []PolicyRule{rule}, "create", entity)
	if err == nil {
		t.Fatal("expected deny error, got nil")
	}
}

func TestPolicyEngine_Evaluate_DenyRule_NoMatch(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	rule := makeRule("r1", "doc.amount > 500", "deny", 10)
	entity := &celTestEntity{Amount: 100, Status: "draft"}

	err := engine.Evaluate(ctx, []PolicyRule{rule}, "create", entity)
	if err != nil {
		t.Fatalf("expected allow (no match), got %v", err)
	}
}

func TestPolicyEngine_Evaluate_AllowRule_Wins(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	// allow rule at higher priority overrides deny at lower
	allow := &stubRule{id: "allow", expression: "doc.amount < 1000", effect: "allow", priority: 20, enabled: true, entity: "order", actions: []string{"create"}}
	deny := &stubRule{id: "deny", expression: "true", effect: "deny", priority: 10, enabled: true, entity: "order", actions: []string{"create"}}
	entity := &celTestEntity{Amount: 500}

	err := engine.Evaluate(ctx, []PolicyRule{allow, deny}, "create", entity)
	if err != nil {
		t.Fatalf("allow rule should win: %v", err)
	}
}

func TestPolicyEngine_Evaluate_DisabledRule_Skipped(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	rule := &stubRule{id: "r1", expression: "true", effect: "deny", priority: 10, enabled: false, entity: "order", actions: []string{"create"}}
	entity := &celTestEntity{Amount: 100}

	err := engine.Evaluate(ctx, []PolicyRule{rule}, "create", entity)
	if err != nil {
		t.Fatalf("disabled rule should be skipped: %v", err)
	}
}

func TestPolicyEngine_Evaluate_PriorityOrder(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	// deny=priority 5, allow=priority 10 — allow should be evaluated first and win
	deny := &stubRule{id: "deny", expression: "true", effect: "deny", priority: 5, enabled: true, entity: "order", actions: []string{"create"}}
	allow := &stubRule{id: "allow", expression: "true", effect: "allow", priority: 10, enabled: true, entity: "order", actions: []string{"create"}}
	entity := &celTestEntity{}

	err := engine.Evaluate(ctx, []PolicyRule{deny, allow}, "create", entity)
	if err != nil {
		t.Fatalf("higher-priority allow should win: %v", err)
	}
}

// --- EvaluateForList tests ---

func TestPolicyEngine_EvaluateForList_FiltersEntities(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	rule := &stubRule{id: "r1", expression: "doc.amount > 500", effect: "deny", priority: 10, enabled: true, entity: "order", actions: []string{"read"}}

	entities := []any{
		&celTestEntity{Amount: 100},
		&celTestEntity{Amount: 1000},
		&celTestEntity{Amount: 200},
	}

	result := engine.EvaluateForList(ctx, []PolicyRule{rule}, entities)
	if len(result) != 2 {
		t.Fatalf("expected 2 entities to pass filter, got %d", len(result))
	}
}

func TestPolicyEngine_EvaluateForList_NoRules_ReturnsAll(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	entities := []any{&celTestEntity{Amount: 100}, &celTestEntity{Amount: 200}}
	result := engine.EvaluateForList(ctx, nil, entities)
	if len(result) != 2 {
		t.Fatalf("expected all entities when no rules, got %d", len(result))
	}
}

// --- InvalidateCache tests ---

func TestPolicyEngine_InvalidateCache(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	rule := makeRule("r1", "true", "deny", 10)
	entity := &celTestEntity{}

	// Prime the cache
	_ = engine.Evaluate(ctx, []PolicyRule{rule}, "create", entity)

	// Should not panic when invalidating
	engine.InvalidateCache("r1")

	// After invalidation, next evaluate should recompile — still works
	err := engine.Evaluate(ctx, []PolicyRule{rule}, "create", entity)
	if err == nil {
		t.Fatal("expected deny after re-evaluation")
	}
}

func TestPolicyEngine_InvalidateAllCache(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	rule := makeRule("r1", "true", "deny", 10)
	entity := &celTestEntity{}

	_ = engine.Evaluate(ctx, []PolicyRule{rule}, "create", entity)
	engine.InvalidateAllCache()

	err := engine.Evaluate(ctx, []PolicyRule{rule}, "create", entity)
	if err == nil {
		t.Fatal("expected deny after full cache clear")
	}
}

// --- celTestEntity for CEL evaluation ---

type celTestEntity struct {
	Amount int64     `db:"amount" json:"amount"`
	Status string    `db:"status" json:"status"`
	Date   time.Time `db:"date" json:"date"`
}
