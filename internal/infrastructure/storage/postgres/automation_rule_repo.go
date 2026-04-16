package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationRuleRepo implements automations.Repository
type AutomationRuleRepo struct{}

// NewAutomationRuleRepo creates a new repository.
func NewAutomationRuleRepo() *AutomationRuleRepo {
	return &AutomationRuleRepo{}
}

// List returns all rules.
func (r *AutomationRuleRepo) List(ctx context.Context, eventType *string) ([]automations.AutomationRule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		SELECT id, name, organization_id, event_type, condition_cel, action_type, action_template, service_account_id, is_active, created_at, updated_at
		FROM sys_automation_rules
		WHERE ($1::text IS NULL OR event_type = $1)
		ORDER BY created_at DESC
	`

	rows, err := q.Query(ctx, query, eventType)
	if err != nil {
		return nil, fmt.Errorf("query list rules: %w", err)
	}
	defer rows.Close()

	var rules []automations.AutomationRule
	for rows.Next() {
		var rule automations.AutomationRule
		var orgID *string
		err := rows.Scan(
			&rule.ID, &rule.Name, &orgID, &rule.EventType, &rule.ConditionCEL,
			&rule.ActionType, &rule.ActionTemplate, &rule.ServiceAccountID,
			&rule.IsActive, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan list rules: %w", err)
		}
		if orgID != nil {
			parsedID, err := id.Parse(*orgID)
			if err == nil {
				rule.OrganizationID = &parsedID
			}
		}
		rules = append(rules, rule)
	}

	return rules, nil
}

// ListActiveByEventType returns active rules for a specific event type.
func (r *AutomationRuleRepo) ListActiveByEventType(ctx context.Context, eventType string) ([]automations.AutomationRule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	// Since we are running in a shared tenant context, the row level security will handle
	// visibility of the rows if applicable. If rules are tenant-specific, this is safe.
	// We might want to eagerly load the active flag here.
	query := `
		SELECT id, name, organization_id, event_type, condition_cel, action_type, action_template, service_account_id, is_active, created_at, updated_at
		FROM sys_automation_rules
		WHERE event_type = $1 AND is_active = TRUE
		ORDER BY created_at ASC
	`

	rows, err := q.Query(ctx, query, eventType)
	if err != nil {
		return nil, fmt.Errorf("query list active rules by event type: %w", err)
	}
	defer rows.Close()

	var rules []automations.AutomationRule
	for rows.Next() {
		var rule automations.AutomationRule
		err := rows.Scan(
			&rule.ID, &rule.Name, &rule.OrganizationID, &rule.EventType, &rule.ConditionCEL,
			&rule.ActionType, &rule.ActionTemplate, &rule.ServiceAccountID,
			&rule.IsActive, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan active rule: %w", err)
		}
		rules = append(rules, rule)
	}

	return rules, nil
}

// GetByID retrieves a rule by ID.
func (r *AutomationRuleRepo) GetByID(ctx context.Context, ruleID id.ID) (*automations.AutomationRule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		SELECT id, name, organization_id, event_type, condition_cel, action_type, action_template, service_account_id, is_active, created_at, updated_at
		FROM sys_automation_rules
		WHERE id = $1
	`

	var rule automations.AutomationRule
	err := q.QueryRow(ctx, query, ruleID).Scan(
		&rule.ID, &rule.Name, &rule.OrganizationID, &rule.EventType, &rule.ConditionCEL,
		&rule.ActionType, &rule.ActionTemplate, &rule.ServiceAccountID,
		&rule.IsActive, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_automation_rules", ruleID)
		}
		return nil, fmt.Errorf("query rule by id: %w", err)
	}

	return &rule, nil
}

// Create creates a new rule.
func (r *AutomationRuleRepo) Create(ctx context.Context, req automations.CreateRuleRequest) (*automations.AutomationRule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		INSERT INTO sys_automation_rules (
			name, organization_id, event_type, condition_cel, action_type, action_template, service_account_id, is_active
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
		RETURNING id, name, organization_id, event_type, condition_cel, action_type, action_template, service_account_id, is_active, created_at, updated_at
	`

	var rule automations.AutomationRule
	err := q.QueryRow(ctx, query,
		req.Name, req.OrganizationID, req.EventType, req.ConditionCEL, req.ActionType, req.ActionTemplate, req.ServiceAccountID, req.IsActive,
	).Scan(
		&rule.ID, &rule.Name, &rule.OrganizationID, &rule.EventType, &rule.ConditionCEL,
		&rule.ActionType, &rule.ActionTemplate, &rule.ServiceAccountID,
		&rule.IsActive, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		// Could handle foreign key violations here (e.g. invalid service_account_id)
		return nil, fmt.Errorf("create automation rule: %w", err)
	}

	return &rule, nil
}

// Update modifies an existing rule.
func (r *AutomationRuleRepo) Update(ctx context.Context, ruleID id.ID, req automations.UpdateRuleRequest) (*automations.AutomationRule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		UPDATE sys_automation_rules
		SET name = $1, organization_id = $2, event_type = $3, condition_cel = $4, action_type = $5, action_template = $6, service_account_id = $7, is_active = $8, updated_at = NOW()
		WHERE id = $9
		RETURNING id, name, organization_id, event_type, condition_cel, action_type, action_template, service_account_id, is_active, created_at, updated_at
	`

	var rule automations.AutomationRule
	err := q.QueryRow(ctx, query,
		req.Name, req.OrganizationID, req.EventType, req.ConditionCEL, req.ActionType, req.ActionTemplate, req.ServiceAccountID, req.IsActive, ruleID,
	).Scan(
		&rule.ID, &rule.Name, &rule.OrganizationID, &rule.EventType, &rule.ConditionCEL,
		&rule.ActionType, &rule.ActionTemplate, &rule.ServiceAccountID,
		&rule.IsActive, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_automation_rules", ruleID)
		}
		return nil, fmt.Errorf("update automation rule: %w", err)
	}

	return &rule, nil
}

// Delete removes a rule.
func (r *AutomationRuleRepo) Delete(ctx context.Context, ruleID id.ID) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `DELETE FROM sys_automation_rules WHERE id = $1`
	cmdTag, err := q.Exec(ctx, query, ruleID)
	if err != nil {
		return fmt.Errorf("delete automation rule: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_automation_rules", ruleID)
	}

	return nil
}

// Ensure interface compliance
var _ automations.Repository = (*AutomationRuleRepo)(nil)
