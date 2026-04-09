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
//   - user:   map with {id, email, roles, isAdmin}
//   - action: string — "create", "read", "update", "delete", "post", "unpost"
//   - now:    current timestamp
type PolicyEngine struct {
	env          *cel.Env
	programCache sync.Map // ruleID+expression hash → *cachedProgram
	stopEviction  context.CancelFunc
}

type cachedProgram struct {
	program    cel.Program
	expression string    // stored to detect stale cache entries
	createdAt  time.Time // for TTL-based eviction
}

// EvictStale removes cached programs older than maxAge.
// Safe to call concurrently. Intended to be called periodically
// from a background goroutine (e.g., tenant manager's eviction loop).
func (e *PolicyEngine) EvictStale(maxAge time.Duration) {
	threshold := time.Now().Add(-maxAge)
	e.programCache.Range(func(key, value any) bool {
		cp := value.(*cachedProgram)
		if cp.createdAt.Before(threshold) {
			e.programCache.Delete(key)
		}
		return true
	})
}

// Default eviction settings for the background CEL cache cleaner.
const (
	// cacheMaxAge is the TTL for compiled CEL programs. After this duration
	// without re-compilation, the entry is evicted from the cache.
	cacheMaxAge = 1 * time.Hour

	// cacheEvictInterval is the tick interval for the background eviction goroutine.
	cacheEvictInterval = 10 * time.Minute
)

// NewPolicyEngine creates a PolicyEngine with a shared CEL environment
// and starts a background goroutine that periodically evicts stale compiled programs
// from the cache (every 10 min, TTL = 1 hour). Call Stop() for graceful cleanup.
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

	ctx, cancel := context.WithCancel(context.Background())
	engine := &PolicyEngine{env: env, stopEviction: cancel}

	// Background cache eviction goroutine.
	go engine.evictionLoop(ctx)

	return engine, nil
}

// Stop cancels the background eviction goroutine.
// Must be called during graceful shutdown (or deferred after NewPolicyEngine).
func (e *PolicyEngine) Stop() {
	if e.stopEviction != nil {
		e.stopEviction()
	}
}

// evictionLoop runs periodically to remove stale entries from the program cache.
func (e *PolicyEngine) evictionLoop(ctx context.Context) {
	ticker := time.NewTicker(cacheEvictInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.EvictStale(cacheMaxAge)
		}
	}
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
// Optimized: user/action/now activation parts are built once outside the entity loop.
func (e *PolicyEngine) EvaluateForList(ctx context.Context, rules []PolicyRule, entities []any) []any {
	applicable := filterAndSort(rules, "read")
	if len(applicable) == 0 {
		return entities
	}

	// Pre-build the activation parts that are the same for every entity
	userMap := userContextToCELMap(ctx)
	now := time.Now().UTC()

	result := make([]any, 0, len(entities))
	for _, ent := range entities {
		activation := map[string]any{
			"doc":    entityToCELMap(ent),
			"user":   userMap,
			"action": "read",
			"now":    now,
		}
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
		createdAt:  time.Now(),
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

// entityToCELMap converts an entity struct to map[string]any using json/db tags.
// Both camelCase (json) and snake_case (db) keys are added so CEL rules can
// reference fields in either convention — e.g. doc.totalAmount or doc.total_amount.
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
	result := make(map[string]any, len(fields)*2)

	for _, fm := range fields {
		if fm.JSONName == "" && fm.DBName == "" {
			continue
		}

		field := v.FieldByIndex(fm.Index)
		val := toCELValue(field)

		// Add camelCase key (json tag) — primary for user-facing CEL rules
		if fm.JSONName != "" {
			result[fm.JSONName] = val
		}
		// Add snake_case key (db tag) as alias, if different from json
		if fm.DBName != "" && fm.DBName != fm.JSONName {
			result[fm.DBName] = val
		}
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
			"isAdmin": false,
		}
	}

	roles := make([]any, len(user.Roles))
	for i, r := range user.Roles {
		roles[i] = r
	}
	return map[string]any{
		"id":      user.UserID,
		"email":   user.Email,
		"roles":   roles,
		"isAdmin": user.IsAdmin,
	}
}

// Ensure cel types are used (avoid import cycle warnings).
var (
	_ = types.BoolType
	_ ref.Val
)
