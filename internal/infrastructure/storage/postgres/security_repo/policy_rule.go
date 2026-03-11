package security_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/storage/postgres"
)

// PolicyRuleRepo implements security_profile.PolicyRuleRepository using PostgreSQL.
type PolicyRuleRepo struct{}

// NewPolicyRuleRepo creates a new PolicyRuleRepo.
func NewPolicyRuleRepo() *PolicyRuleRepo {
	return &PolicyRuleRepo{}
}

func (r *PolicyRuleRepo) builder() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

func (r *PolicyRuleRepo) querier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

const policyRuleTable = "security_policy_rules"

var policyRuleCols = []string{
	"id", "profile_id", "name", "description", "entity_name",
	"actions", "expression", "effect", "priority", "enabled",
	"created_at", "updated_at",
}

// Create inserts a new policy rule.
func (r *PolicyRuleRepo) Create(ctx context.Context, rule *security_profile.PolicyRule) error {
	q := r.querier(ctx)

	if id.IsNil(rule.ID) {
		rule.ID = id.New()
	}

	sql, args, err := r.builder().
		Insert(policyRuleTable).
		Columns("id", "profile_id", "name", "description", "entity_name",
			"actions", "expression", "effect", "priority", "enabled").
		Values(rule.ID, rule.ProfileID, rule.Name, rule.Description, rule.EntityName,
			rule.Actions, rule.Expression, rule.Effect, rule.Priority, rule.Enabled).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	if _, err := q.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert policy rule: %w", err)
	}
	return nil
}

// GetByID retrieves a policy rule by ID.
func (r *PolicyRuleRepo) GetByID(ctx context.Context, ruleID id.ID) (*security_profile.PolicyRule, error) {
	q := r.querier(ctx)

	sql, args, err := r.builder().
		Select(policyRuleCols...).
		From(policyRuleTable).
		Where(squirrel.Eq{"id": ruleID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var rule security_profile.PolicyRule
	if err := pgxscan.Get(ctx, q, &rule, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("policy_rule", ruleID.String())
		}
		return nil, fmt.Errorf("get policy rule: %w", err)
	}
	return &rule, nil
}

// Update modifies an existing policy rule.
func (r *PolicyRuleRepo) Update(ctx context.Context, rule *security_profile.PolicyRule) error {
	q := r.querier(ctx)

	sql, args, err := r.builder().
		Update(policyRuleTable).
		Set("name", rule.Name).
		Set("description", rule.Description).
		Set("entity_name", rule.EntityName).
		Set("actions", rule.Actions).
		Set("expression", rule.Expression).
		Set("effect", rule.Effect).
		Set("priority", rule.Priority).
		Set("enabled", rule.Enabled).
		Set("updated_at", squirrel.Expr("now()")).
		Where(squirrel.Eq{"id": rule.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update policy rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewNotFound("policy_rule", rule.ID.String())
	}
	return nil
}

// Delete removes a policy rule.
func (r *PolicyRuleRepo) Delete(ctx context.Context, ruleID id.ID) error {
	q := r.querier(ctx)

	sql, args, err := r.builder().
		Delete(policyRuleTable).
		Where(squirrel.Eq{"id": ruleID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("delete policy rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewNotFound("policy_rule", ruleID.String())
	}
	return nil
}

// ListByProfileID returns all rules for a profile, ordered by priority DESC.
func (r *PolicyRuleRepo) ListByProfileID(ctx context.Context, profileID id.ID) ([]*security_profile.PolicyRule, error) {
	q := r.querier(ctx)

	sql, args, err := r.builder().
		Select(policyRuleCols...).
		From(policyRuleTable).
		Where(squirrel.Eq{"profile_id": profileID}).
		OrderBy("priority DESC", "created_at ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list query: %w", err)
	}

	var rules []*security_profile.PolicyRule
	if err := pgxscan.Select(ctx, q, &rules, sql, args...); err != nil {
		return nil, fmt.Errorf("list policy rules: %w", err)
	}
	return rules, nil
}

// Compile-time check.
var _ security_profile.PolicyRuleRepository = (*PolicyRuleRepo)(nil)

// Suppress unused import.
var _ pgx.Tx
