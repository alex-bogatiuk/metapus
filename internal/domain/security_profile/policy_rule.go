package security_profile

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// PolicyRule is a CEL-based authorization rule attached to a SecurityProfile.
// When evaluated, the CEL expression receives document/entity data, user context,
// action name, and current timestamp. The expression must return a bool.
//
// Semantics:
//   - effect="deny":  if expression evaluates to true → operation is DENIED
//   - effect="allow": if expression evaluates to true → operation is ALLOWED
//
// Rules are evaluated in priority order (descending). First matching rule wins.
// If no rule matches → operation is allowed (RLS/FLS already filtered).
type PolicyRule struct {
	ID          id.ID     `db:"id" json:"id"`
	ProfileID   id.ID     `db:"profile_id" json:"profileId"`
	Name        string    `db:"name" json:"name"`
	Description string    `db:"description" json:"description,omitempty"`
	EntityName  string    `db:"entity_name" json:"entityName"`
	Actions     []string  `db:"actions" json:"actions"`
	Expression  string    `db:"expression" json:"expression"`
	Effect      string    `db:"effect" json:"effect"`
	Priority    int       `db:"priority" json:"priority"`
	Enabled     bool      `db:"enabled" json:"enabled"`
	CreatedAt   time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt   time.Time `db:"updated_at" json:"updatedAt"`
}

// Effect constants.
const (
	EffectDeny  = "deny"
	EffectAllow = "allow"
)

// Validate checks domain invariants (no DB access).
func (r *PolicyRule) Validate(_ context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if r.EntityName == "" {
		return apperror.NewValidation("entity_name is required").WithDetail("field", "entityName")
	}
	if r.Expression == "" {
		return apperror.NewValidation("expression is required").WithDetail("field", "expression")
	}
	if len(r.Actions) == 0 {
		return apperror.NewValidation("at least one action is required").WithDetail("field", "actions")
	}
	if r.Effect != EffectDeny && r.Effect != EffectAllow {
		return apperror.NewValidation("effect must be 'deny' or 'allow'").WithDetail("field", "effect")
	}
	return nil
}

// MatchesAction returns true if the rule applies to the given action.
// Wildcard "*" matches all actions.
func (r *PolicyRule) MatchesAction(action string) bool {
	for _, a := range r.Actions {
		if a == "*" || a == action {
			return true
		}
	}
	return false
}

// MatchesEntity returns true if the rule applies to the given entity name.
// Wildcard "*" matches all entities.
func (r *PolicyRule) MatchesEntity(entityName string) bool {
	return r.EntityName == "*" || r.EntityName == entityName
}

// --- security.PolicyRule interface implementation ---

func (r *PolicyRule) GetID() string         { return r.ID.String() }
func (r *PolicyRule) GetExpression() string { return r.Expression }
func (r *PolicyRule) GetEffect() string     { return r.Effect }
func (r *PolicyRule) GetPriority() int      { return r.Priority }
func (r *PolicyRule) GetEnabled() bool      { return r.Enabled }
