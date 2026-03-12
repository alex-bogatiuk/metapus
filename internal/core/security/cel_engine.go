package security

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
)

// PolicyRule is a minimal interface to avoid circular imports with security_profile package.
// The actual PolicyRule struct lives in domain/security_profile.
type PolicyRule interface {
	GetID() string
	GetExpression() string
	GetEffect() string // "deny" or "allow"
	GetPriority() int
	GetEnabled() bool
	MatchesAction(action string) bool
	MatchesEntity(entityName string) bool
}

// PolicyEngine compiles and evaluates CEL expressions for fine-grained authorization.
//
// CEL environment variables available in expressions:
//   - doc:    map of entity fields (json/db tag names → values)
//   - user:   map with {id, email, roles, orgIds, isAdmin}
//   - action: string — "create", "read", "update", "delete", "post", "unpost"
//   - now:    current timestamp
type PolicyEngine struct {
	env          *cel.Env
	programCache sync.Map // ruleID+expression hash → *cachedProgram
}

type cachedProgram struct {
	program    cel.Program
	expression string // stored to detect stale cache entries
}

// NewPolicyEngine creates a PolicyEngine with a shared CEL environment.
func NewPolicyEngine() (*PolicyEngine, error) {
	env, err := cel.NewEnv(
		cel.Variable("doc", cel.DynType),
		cel.Variable("user", cel.DynType),
		cel.Variable("action", cel.StringType),
		cel.Variable("now", cel.TimestampType),
	)
	if err != nil {
		return nil, fmt.Errorf("cel: create environment: %w", err)
	}
	return &PolicyEngine{env: env}, nil
}

// Compile validates and compiles a CEL expression.
// Returns nil if valid, an error describing the syntax/type problem otherwise.
// Used at rule-save time to reject invalid expressions early.
func (e *PolicyEngine) Compile(expression string) error {
	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return apperror.NewValidation(fmt.Sprintf("invalid CEL expression: %s", issues.Err())).
			WithDetail("field", "expression")
	}
	// Verify the output type is bool
	if ast.OutputType() != cel.BoolType {
		return apperror.NewValidation(
			fmt.Sprintf("CEL expression must return bool, got %s", ast.OutputType()),
		).WithDetail("field", "expression")
	}
	return nil
}

// Evaluate runs all applicable rules against the entity for the given action.
// Rules are sorted by priority (descending). First matching rule wins.
// Returns nil if allowed, apperror.Forbidden if denied.
func (e *PolicyEngine) Evaluate(ctx context.Context, rules []PolicyRule, action string, entity any) error {
	applicable := filterAndSort(rules, action)
	if len(applicable) == 0 {
		return nil
	}

	activation := e.buildActivation(ctx, action, entity)

	for _, rule := range applicable {
		result, err := e.evalRule(rule, activation)
		if err != nil {
			// Fail-closed: evaluation error → deny
			return apperror.NewForbidden(
				fmt.Sprintf("policy rule '%s' evaluation error: %v", rule.GetID(), err),
			)
		}
		if !result {
			continue // expression returned false — rule doesn't match
		}
		// Expression returned true — apply effect
		if rule.GetEffect() == "deny" {
			return apperror.NewForbidden(
				fmt.Sprintf("action '%s' denied by policy rule", action),
			).WithDetail("rule_id", rule.GetID())
		}
		if rule.GetEffect() == "allow" {
			return nil // explicitly allowed
		}
	}

	// No rule matched → default allow (RLS/FLS already filtered)
	return nil
}

// EvaluateForList filters a slice of entities, removing those denied by "read" rules.
// Entities that cause evaluation errors are also removed (fail-closed).
func (e *PolicyEngine) EvaluateForList(ctx context.Context, rules []PolicyRule, entities []any) []any {
	applicable := filterAndSort(rules, "read")
	if len(applicable) == 0 {
		return entities
	}

	result := make([]any, 0, len(entities))
	for _, ent := range entities {
		activation := e.buildActivation(ctx, "read", ent)
		if e.isAllowedByRules(applicable, activation) {
			result = append(result, ent)
		}
	}
	return result
}

// isAllowedByRules checks if the activation is allowed by the given sorted rules.
func (e *PolicyEngine) isAllowedByRules(rules []PolicyRule, activation map[string]any) bool {
	for _, rule := range rules {
		result, err := e.evalRule(rule, activation)
		if err != nil {
			return false // fail-closed
		}
		if !result {
			continue
		}
		if rule.GetEffect() == "deny" {
			return false
		}
		if rule.GetEffect() == "allow" {
			return true
		}
	}
	return true // default allow
}

// buildActivation creates the CEL activation map from context and entity.
func (e *PolicyEngine) buildActivation(ctx context.Context, action string, entity any) map[string]any {
	return map[string]any{
		"doc":    entityToCELMap(entity),
		"user":   userContextToCELMap(ctx),
		"action": action,
		"now":    time.Now().UTC(),
	}
}

// evalRule compiles (or retrieves from cache) and evaluates a single rule.
func (e *PolicyEngine) evalRule(rule PolicyRule, activation map[string]any) (bool, error) {
	program, err := e.getOrCompile(rule)
	if err != nil {
		return false, err
	}

	out, _, err := program.Eval(activation)
	if err != nil {
		return false, fmt.Errorf("eval: %w", err)
	}

	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expression returned %T, expected bool", out.Value())
	}
	return b, nil
}

// getOrCompile returns a compiled CEL program, using cache when possible.
func (e *PolicyEngine) getOrCompile(rule PolicyRule) (cel.Program, error) {
	cacheKey := rule.GetID()
	expr := rule.GetExpression()

	if cached, ok := e.programCache.Load(cacheKey); ok {
		cp := cached.(*cachedProgram)
		if cp.expression == expr {
			return cp.program, nil
		}
		// Expression changed — recompile
	}

	ast, issues := e.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile: %w", issues.Err())
	}

	program, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program: %w", err)
	}

	e.programCache.Store(cacheKey, &cachedProgram{
		program:    program,
		expression: expr,
	})

	return program, nil
}

// EvalExpression compiles and evaluates a single CEL expression against the given activation map.
// Used by the CEL sandbox for testing expressions without requiring a full PolicyRule.
// Returns the boolean result or an error.
func (e *PolicyEngine) EvalExpression(expression string, activation map[string]any) (bool, error) {
	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("compile: %w", issues.Err())
	}

	program, err := e.env.Program(ast)
	if err != nil {
		return false, fmt.Errorf("program: %w", err)
	}

	out, _, err := program.Eval(activation)
	if err != nil {
		return false, fmt.Errorf("eval: %w", err)
	}

	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expression returned %T, expected bool", out.Value())
	}
	return b, nil
}

// InvalidateCache removes a cached program for a rule (e.g., after rule update).
func (e *PolicyEngine) InvalidateCache(ruleID string) {
	e.programCache.Delete(ruleID)
}

// InvalidateAllCache clears the entire program cache.
func (e *PolicyEngine) InvalidateAllCache() {
	e.programCache.Range(func(key, _ any) bool {
		e.programCache.Delete(key)
		return true
	})
}

// --- helpers ---

// filterAndSort returns enabled rules matching the action, sorted by priority DESC.
func filterAndSort(rules []PolicyRule, action string) []PolicyRule {
	var applicable []PolicyRule
	for _, r := range rules {
		if r.GetEnabled() && r.MatchesAction(action) {
			applicable = append(applicable, r)
		}
	}
	sort.Slice(applicable, func(i, j int) bool {
		return applicable[i].GetPriority() > applicable[j].GetPriority()
	})
	return applicable
}

// entityToCELMap converts an entity struct to map[string]any using db/json tags.
// Reuses the globalFieldCache from field_masker.go.
func entityToCELMap(entity any) map[string]any {
	if entity == nil {
		return map[string]any{}
	}

	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return map[string]any{}
	}

	fields := globalFieldCache.getFields(v.Type())
	result := make(map[string]any, len(fields))

	for _, fm := range fields {
		key := fm.DBName
		if key == "" {
			key = fm.JSONName
		}
		if key == "" {
			continue
		}

		field := v.FieldByIndex(fm.Index)
		result[key] = toCELValue(field)
	}

	return result
}

// toCELValue converts a reflect.Value to a CEL-friendly Go value.
func toCELValue(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}

	// Handle pointer types
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		return toCELValue(v.Elem())
	}

	iface := v.Interface()

	// Convert time.Time to CEL timestamp
	if t, ok := iface.(time.Time); ok {
		return t
	}

	// Convert types implementing fmt.Stringer (e.g., id.ID, uuid.UUID)
	if s, ok := iface.(fmt.Stringer); ok {
		str := s.String()
		// Don't convert zero UUIDs to string "00000000-..."
		if str == "00000000-0000-0000-0000-000000000000" {
			return ""
		}
		return str
	}

	// Slices → []any for CEL
	if v.Kind() == reflect.Slice {
		if v.IsNil() {
			return []any{}
		}
		result := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = toCELValue(v.Index(i))
		}
		return result
	}

	return iface
}

// userContextToCELMap builds the "user" variable for CEL from request context.
func userContextToCELMap(ctx context.Context) map[string]any {
	user := appctx.GetUser(ctx)
	if user == nil {
		return map[string]any{
			"id":      "",
			"email":   "",
			"roles":   []any{},
			"orgIds":  []any{},
			"isAdmin": false,
		}
	}

	roles := make([]any, len(user.Roles))
	for i, r := range user.Roles {
		roles[i] = r
	}
	orgIDs := make([]any, len(user.OrgIDs))
	for i, o := range user.OrgIDs {
		orgIDs[i] = o
	}

	return map[string]any{
		"id":      user.UserID,
		"email":   user.Email,
		"roles":   roles,
		"orgIds":  orgIDs,
		"isAdmin": user.IsAdmin,
	}
}

// Ensure cel types are used (avoid import cycle warnings).
var (
	_ = types.BoolType
	_ ref.Val
)
