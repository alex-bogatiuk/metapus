// Package security applies row-level, CEL and field policies of the
// request's security profile.
package security

import (
	"context"

	core "github.com/alex-bogatiuk/metapus/internal/core/security"
)

type (
	// FieldPolicy describes which fields a role is allowed to read or write
	// for a specific entity and action.
	//
	// AllowedFields uses a simple DSL:
	//   - ["*"] — all fields allowed
	//   - ["-status", "-organization_id"] — all except these fields
	//   - ["quantity", "unit_price"] — only these specific fields
	//
	// TableParts optionally restricts which columns within a table part (tabular section)
	// can be modified. Key = table part name (e.g. "items"), value = allowed column names.
	FieldPolicy = core.FieldPolicy
	// PolicyEngine compiles and evaluates CEL expressions for fine-grained authorization.
	//
	// CEL environment variables available in expressions:
	//   - doc:    map of entity fields (json/db tag names → values)
	//   - user:   map with {id, email, roles, isAdmin}
	//   - action: string — "create", "read", "update", "delete", "post", "unpost"
	//   - now:    current timestamp
	PolicyEngine = core.PolicyEngine
	// PolicyRule is a minimal interface to avoid circular imports with security_profile package.
	// The actual PolicyRule struct lives in domain/security_profile.
	PolicyRule = core.PolicyRule
)

// CheckCELPolicy evaluates CEL policy rules for the given action and entity.
// Returns nil if no PolicyEngine is provided, no rules match, or evaluation allows the action.
func CheckCELPolicy(ctx context.Context, engine *core.PolicyEngine, entityName string, action string, p4 any) error {
	return core.CheckCELPolicy(ctx, engine, entityName, action, p4)
}

// CheckRLSAccess verifies that the current DataScope allows access to the entity.
// If the entity implements RLSDimensionable, its dimensions are checked against the scope.
// Returns nil if access is allowed, apperror.Forbidden otherwise.
func CheckRLSAccess(ctx context.Context, entityName string, p2 any) error {
	return core.CheckRLSAccess(ctx, entityName, p2)
}

// GetDataScope returns DataScope from context.
// If not found, creates one from UserContext (fail-closed).
func GetDataScope(ctx context.Context) *core.DataScope { return core.GetDataScope(ctx) }

// GetFieldPolicy returns the FieldPolicy for a specific entity and action.
// Returns nil if no policy is defined (no FLS restrictions).
func GetFieldPolicy(ctx context.Context, entityName string, action string) *core.FieldPolicy {
	return core.GetFieldPolicy(ctx, entityName, action)
}

// NewPolicyEngine creates a PolicyEngine with a shared CEL environment
// and starts a background goroutine that periodically evicts stale compiled programs
// from the cache (every 10 min, TTL = 1 hour). Call Stop() for graceful cleanup.
func NewPolicyEngine() (*core.PolicyEngine, error) { return core.NewPolicyEngine() }

// ProjectForRead isolates presentation mutations from a domain entity, including
// its pointers, attributes and table parts. Internal immutable scalar types keep
// their concrete representation; no JSON round trip changes their codecs.
func ProjectForRead[T any](source T, policy *core.FieldPolicy) T {
	return core.ProjectForRead[T](source, policy)
}

// ValidateWrite compares oldEntity and newEntity, checking if any changed fields
// violate the given FieldPolicy. Returns apperror.Forbidden if a restricted field was modified.
//
// Both entities must be the same type (or pointers to the same type).
// Comparison is done via fmt.Sprintf("%v") for simplicity — sufficient for
// primitive types (string, int, ID, bool) used in document headers.
func ValidateWrite(oldEntity any, newEntity any, policy *core.FieldPolicy) error {
	return core.ValidateWrite(oldEntity, newEntity, policy)
}

// WithFieldPolicies adds field policies map to context.
// Key format: "entity_name:action" (e.g. "crypto_invoice:read").
func WithFieldPolicies(ctx context.Context, policies map[string]*core.FieldPolicy) context.Context {
	return core.WithFieldPolicies(ctx, policies)
}

// WithPolicyRules adds policy rules to the request context.
func WithPolicyRules(ctx context.Context, rules []core.PolicyRule) context.Context {
	return core.WithPolicyRules(ctx, rules)
}
