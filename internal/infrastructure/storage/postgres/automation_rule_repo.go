package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationRuleRepo implements automations.RuleRepository.
type AutomationRuleRepo struct{}

// NewAutomationRuleRepo creates a new repository.
func NewAutomationRuleRepo() *AutomationRuleRepo {
	return &AutomationRuleRepo{}
}

const ruleSelectCols = `id, name, description, trigger_type, event_type, target_entities, 
	condition_cel, reaction_type, notif_severity, message_format, action_template, chain_rule_ids,
	priority, max_retries, cooldown_seconds, organization_id, is_active,
	execution_count, error_count, last_executed_at,
	deletion_mark, version, created_at, updated_at`

// scanRule scans a pgx.Row (also satisfied by pgx.Rows) into a Rule struct.
func scanRule(row pgx.Row) (*automations.Rule, error) {
	var r automations.Rule
	var targetEntities []string
	var chainIDsJSON []byte

	err := row.Scan(
		&r.ID, &r.Name, &r.Description, &r.TriggerType, &r.EventType, &targetEntities,
		&r.ConditionCEL, &r.ReactionType, &r.NotifSeverity, &r.MessageFormat, &r.ActionTemplate, &chainIDsJSON,
		&r.Priority, &r.MaxRetries, &r.CooldownSecs, &r.OrganizationID, &r.IsActive,
		&r.ExecutionCount, &r.ErrorCount, &r.LastExecutedAt,
		&r.DeletionMark, &r.Version, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	r.TargetEntities = targetEntities

	if len(chainIDsJSON) > 0 {
		var chainStrings []string
		if err := json.Unmarshal(chainIDsJSON, &chainStrings); err == nil {
			for _, s := range chainStrings {
				parsed, parseErr := id.Parse(s)
				if parseErr == nil {
					r.ChainRuleIDs = append(r.ChainRuleIDs, parsed)
				}
			}
		}
	}

	return &r, nil
}

// List returns all non-deleted rules.
func (r *AutomationRuleRepo) List(ctx context.Context, eventType *string) ([]automations.Rule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`
		SELECT %s
		FROM sys_automation_rules
		WHERE deletion_mark = FALSE AND ($1::text IS NULL OR event_type = $1)
		ORDER BY priority DESC, created_at DESC
	`, ruleSelectCols)

	rows, err := q.Query(ctx, query, eventType)
	if err != nil {
		return nil, fmt.Errorf("query list rules: %w", err)
	}
	defer rows.Close()

	var rules []automations.Rule
	var ruleIDs []id.ID
	for rows.Next() {
		rule, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan rule: %w", scanErr)
		}
		ruleIDs = append(ruleIDs, rule.ID)
		rules = append(rules, *rule)
	}

	if len(rules) == 0 {
		return rules, nil
	}

	// Batch-load all subscribers in one query instead of N+1
	allSubs, subErr := r.loadSubscribersBatch(ctx, ruleIDs)
	if subErr != nil {
		return nil, fmt.Errorf("batch load subscribers: %w", subErr)
	}

	// Group subscribers by rule ID
	subsByRule := make(map[id.ID][]automations.Subscriber, len(ruleIDs))
	for _, s := range allSubs {
		subsByRule[s.RuleID] = append(subsByRule[s.RuleID], s)
	}
	for i := range rules {
		rules[i].Subscribers = subsByRule[rules[i].ID]
	}

	return rules, nil
}

// ListActiveByEvent is the hot path for Engine.Evaluate.
// Matches rules where event_type equals the given action AND
// (target_entities is NULL/wildcard OR contains the given entityName).
// Uses batch subscriber loading to avoid N+1 queries.
func (r *AutomationRuleRepo) ListActiveByEvent(ctx context.Context, eventType string, entityName string) ([]automations.Rule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`
		SELECT %s
		FROM sys_automation_rules
		WHERE event_type = $1
		  AND (target_entities IS NULL OR $2 = ANY(target_entities))
		  AND is_active = TRUE AND deletion_mark = FALSE
		ORDER BY priority DESC, created_at ASC
	`, ruleSelectCols)

	rows, err := q.Query(ctx, query, eventType, entityName)
	if err != nil {
		return nil, fmt.Errorf("query active rules by event: %w", err)
	}
	defer rows.Close()

	var rules []automations.Rule
	var ruleIDs []id.ID
	for rows.Next() {
		rule, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan active rule: %w", scanErr)
		}
		ruleIDs = append(ruleIDs, rule.ID)
		rules = append(rules, *rule)
	}

	if len(rules) == 0 {
		return rules, nil
	}

	// Batch-load all subscribers in one query instead of N+1
	allSubs, subErr := r.loadSubscribersBatch(ctx, ruleIDs)
	if subErr != nil {
		return nil, fmt.Errorf("batch load subscribers: %w", subErr)
	}

	// Group subscribers by rule ID
	subsByRule := make(map[id.ID][]automations.Subscriber, len(ruleIDs))
	for _, s := range allSubs {
		subsByRule[s.RuleID] = append(subsByRule[s.RuleID], s)
	}
	for i := range rules {
		rules[i].Subscribers = subsByRule[rules[i].ID]
	}

	return rules, nil
}

// ListActiveByTriggerType returns active rules by trigger type (e.g. "scheduled").
// Uses batch subscriber loading to avoid N+1 queries (same as ListActiveByEventType).
func (r *AutomationRuleRepo) ListActiveByTriggerType(ctx context.Context, triggerType automations.TriggerType) ([]automations.Rule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`
		SELECT %s
		FROM sys_automation_rules
		WHERE trigger_type = $1 AND is_active = TRUE AND deletion_mark = FALSE
		ORDER BY priority DESC
	`, ruleSelectCols)

	rows, err := q.Query(ctx, query, triggerType)
	if err != nil {
		return nil, fmt.Errorf("query active rules by trigger type: %w", err)
	}
	defer rows.Close()

	var rules []automations.Rule
	var ruleIDs []id.ID
	for rows.Next() {
		rule, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan rule: %w", scanErr)
		}
		ruleIDs = append(ruleIDs, rule.ID)
		rules = append(rules, *rule)
	}

	if len(rules) == 0 {
		return rules, nil
	}

	// Batch-load all subscribers in one query instead of N+1
	allSubs, subErr := r.loadSubscribersBatch(ctx, ruleIDs)
	if subErr != nil {
		return nil, fmt.Errorf("batch load subscribers: %w", subErr)
	}

	// Group subscribers by rule ID
	subsByRule := make(map[id.ID][]automations.Subscriber, len(ruleIDs))
	for _, s := range allSubs {
		subsByRule[s.RuleID] = append(subsByRule[s.RuleID], s)
	}
	for i := range rules {
		rules[i].Subscribers = subsByRule[rules[i].ID]
	}

	return rules, nil
}

// GetByID retrieves a rule with its subscribers.
func (r *AutomationRuleRepo) GetByID(ctx context.Context, ruleID id.ID) (*automations.Rule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`SELECT %s FROM sys_automation_rules WHERE id = $1 AND deletion_mark = FALSE`, ruleSelectCols)

	rule, err := scanRule(q.QueryRow(ctx, query, ruleID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_automation_rules", ruleID)
		}
		return nil, fmt.Errorf("query rule by id: %w", err)
	}

	// Load subscribers
	subs, subErr := r.loadSubscribers(ctx, ruleID)
	if subErr != nil {
		return nil, fmt.Errorf("load subscribers: %w", subErr)
	}
	rule.Subscribers = subs

	return rule, nil
}

// Create creates a rule and its subscribers in a single transaction.
func (r *AutomationRuleRepo) Create(ctx context.Context, req automations.CreateRuleRequest) (*automations.Rule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	var chainIDStrings []string
	for _, cid := range req.ChainRuleIDs {
		chainIDStrings = append(chainIDStrings, cid.String())
	}

	query := fmt.Sprintf(`
		INSERT INTO sys_automation_rules (
			name, description, trigger_type, event_type, target_entities,
			condition_cel, reaction_type, notif_severity, message_format, action_template, chain_rule_ids,
			priority, max_retries, cooldown_seconds, organization_id, is_active
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16
		)
		RETURNING %s
	`, ruleSelectCols)

	// For wildcard matching, empty target_entities should be inserted as NULL
	var targetEntities []string
	if len(req.TargetEntities) > 0 {
		targetEntities = req.TargetEntities
	}

	rule, err := scanRule(q.QueryRow(ctx, query,
		req.Name, req.Description, req.TriggerType, req.EventType, targetEntities,
		req.ConditionCEL, req.ReactionType, req.NotifSeverity, req.MessageFormat, req.ActionTemplate, chainIDStrings,
		req.Priority, req.MaxRetries, req.CooldownSecs, req.OrganizationID, req.IsActive,
	))
	if err != nil {
		return nil, fmt.Errorf("create rule: %w", err)
	}

	// Insert subscribers
	subs, subErr := r.insertSubscribers(ctx, rule.ID, req.Subscribers)
	if subErr != nil {
		return nil, fmt.Errorf("insert subscribers: %w", subErr)
	}
	rule.Subscribers = subs

	return rule, nil
}

// Update updates a rule and replaces all subscribers atomically.
func (r *AutomationRuleRepo) Update(ctx context.Context, ruleID id.ID, req automations.UpdateRuleRequest) (*automations.Rule, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	var chainIDStrings []string
	for _, cid := range req.ChainRuleIDs {
		chainIDStrings = append(chainIDStrings, cid.String())
	}

	query := fmt.Sprintf(`
		UPDATE sys_automation_rules
		SET name = $1, description = $2, trigger_type = $3, event_type = $4, target_entities = $5,
			condition_cel = $6, reaction_type = $7, notif_severity = $8, message_format = $9, action_template = $10, chain_rule_ids = $11,
			priority = $12, max_retries = $13, cooldown_seconds = $14, organization_id = $15, is_active = $16,
			version = version + 1
		WHERE id = $17 AND version = $18 AND deletion_mark = FALSE
		RETURNING %s
	`, ruleSelectCols)

	// For wildcard matching, empty target_entities should be updated to NULL
	var targetEntities []string
	if len(req.TargetEntities) > 0 {
		targetEntities = req.TargetEntities
	}

	rule, err := scanRule(q.QueryRow(ctx, query,
		req.Name, req.Description, req.TriggerType, req.EventType, targetEntities,
		req.ConditionCEL, req.ReactionType, req.NotifSeverity, req.MessageFormat, req.ActionTemplate, chainIDStrings,
		req.Priority, req.MaxRetries, req.CooldownSecs, req.OrganizationID, req.IsActive,
		ruleID, req.Version,
	))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewConcurrentModification("sys_automation_rules", ruleID)
		}
		return nil, fmt.Errorf("update rule: %w", err)
	}

	// Replace subscribers: delete all + insert new
	if _, delErr := q.Exec(ctx, `DELETE FROM sys_automation_subscribers WHERE rule_id = $1`, ruleID); delErr != nil {
		return nil, fmt.Errorf("delete old subscribers: %w", delErr)
	}

	subs, subErr := r.insertSubscribers(ctx, ruleID, req.Subscribers)
	if subErr != nil {
		return nil, fmt.Errorf("insert subscribers: %w", subErr)
	}
	rule.Subscribers = subs

	return rule, nil
}

// Delete marks a rule for soft deletion (cascades to subscribers via DB FK).
func (r *AutomationRuleRepo) Delete(ctx context.Context, ruleID id.ID) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	// Hard-delete subscribers first (they have no standalone lifecycle)
	if _, err := q.Exec(ctx, `DELETE FROM sys_automation_subscribers WHERE rule_id = $1`, ruleID); err != nil {
		return fmt.Errorf("delete subscribers: %w", err)
	}

	query := `UPDATE sys_automation_rules SET deletion_mark = TRUE WHERE id = $1 AND deletion_mark = FALSE`
	cmdTag, err := q.Exec(ctx, query, ruleID)
	if err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_automation_rules", ruleID)
	}

	return nil
}

// Toggle switches is_active and returns the new state.
func (r *AutomationRuleRepo) Toggle(ctx context.Context, ruleID id.ID) (bool, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		UPDATE sys_automation_rules
		SET is_active = NOT is_active, version = version + 1
		WHERE id = $1 AND deletion_mark = FALSE
		RETURNING is_active
	`

	var isActive bool
	if err := q.QueryRow(ctx, query, ruleID).Scan(&isActive); err != nil {
		if err == pgx.ErrNoRows {
			return false, apperror.NewNotFound("sys_automation_rules", ruleID)
		}
		return false, fmt.Errorf("toggle rule: %w", err)
	}

	return isActive, nil
}

// IncrementStats atomically increments execution_count (and error_count if isError).
func (r *AutomationRuleRepo) IncrementStats(ctx context.Context, ruleID id.ID, isError bool) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		UPDATE sys_automation_rules
		SET execution_count = execution_count + 1,
			error_count = error_count + CASE WHEN $1 THEN 1 ELSE 0 END,
			last_executed_at = NOW()
		WHERE id = $2
	`

	_, err := q.Exec(ctx, query, isError, ruleID)
	return err
}

// loadSubscribers loads all subscribers for a rule with denormalized display names.
func (r *AutomationRuleRepo) loadSubscribers(ctx context.Context, ruleID id.ID) ([]automations.Subscriber, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		SELECT s.id, s.rule_id, s.subscriber_type, s.channel_id, s.user_id,
			s.role_name, s.doc_field_path, s.delivery_method, s.idx,
			c.name AS channel_name,
			CONCAT_WS(' ', u.first_name, u.last_name) AS user_name
		FROM sys_automation_subscribers s
		LEFT JOIN sys_automation_channels c ON c.id = s.channel_id
		LEFT JOIN users u ON u.id = s.user_id
		WHERE s.rule_id = $1
		ORDER BY s.idx
	`

	rows, err := q.Query(ctx, query, ruleID)
	if err != nil {
		return nil, fmt.Errorf("query subscribers: %w", err)
	}
	defer rows.Close()

	var subs []automations.Subscriber
	for rows.Next() {
		var s automations.Subscriber
		err := rows.Scan(
			&s.ID, &s.RuleID, &s.SubscriberType, &s.ChannelID, &s.UserID,
			&s.RoleName, &s.DocFieldPath, &s.DeliveryMethod, &s.Idx,
			&s.ChannelName, &s.UserName,
		)
		if err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		subs = append(subs, s)
	}

	return subs, nil
}

// loadSubscribersBatch loads subscribers for multiple rules in a single query.
// Used by ListActiveByEventType to avoid N+1 queries on the hot path.
func (r *AutomationRuleRepo) loadSubscribersBatch(ctx context.Context, ruleIDs []id.ID) ([]automations.Subscriber, error) {
	if len(ruleIDs) == 0 {
		return nil, nil
	}

	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		SELECT s.id, s.rule_id, s.subscriber_type, s.channel_id, s.user_id,
			s.role_name, s.doc_field_path, s.delivery_method, s.idx,
			c.name AS channel_name,
			CONCAT_WS(' ', u.first_name, u.last_name) AS user_name
		FROM sys_automation_subscribers s
		LEFT JOIN sys_automation_channels c ON c.id = s.channel_id
		LEFT JOIN users u ON u.id = s.user_id
		WHERE s.rule_id = ANY($1)
		ORDER BY s.rule_id, s.idx
	`

	rows, err := q.Query(ctx, query, ruleIDs)
	if err != nil {
		return nil, fmt.Errorf("query subscribers batch: %w", err)
	}
	defer rows.Close()

	var subs []automations.Subscriber
	for rows.Next() {
		var s automations.Subscriber
		err := rows.Scan(
			&s.ID, &s.RuleID, &s.SubscriberType, &s.ChannelID, &s.UserID,
			&s.RoleName, &s.DocFieldPath, &s.DeliveryMethod, &s.Idx,
			&s.ChannelName, &s.UserName,
		)
		if err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		subs = append(subs, s)
	}

	return subs, nil
}

// insertSubscribers inserts all subscriber rows in a single multi-VALUES INSERT.
func (r *AutomationRuleRepo) insertSubscribers(ctx context.Context, ruleID id.ID, inputs []automations.SubscriberInput) ([]automations.Subscriber, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	// Build multi-VALUES INSERT: ($1,$2,...,$8), ($9,$10,...,$16), ...
	const colsPerRow = 8
	var sb strings.Builder
	sb.WriteString(`INSERT INTO sys_automation_subscribers (
		rule_id, subscriber_type, channel_id, user_id, role_name, doc_field_path, delivery_method, idx
	) VALUES `)

	args := make([]any, 0, len(inputs)*colsPerRow)
	for i, input := range inputs {
		if i > 0 {
			sb.WriteString(", ")
		}
		base := i * colsPerRow
		fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8)
		args = append(args, ruleID, input.SubscriberType, input.ChannelID, input.UserID,
			input.RoleName, input.DocFieldPath, input.DeliveryMethod, input.Idx)
	}
	sb.WriteString(" RETURNING id")

	rows, err := q.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("batch insert subscribers: %w", err)
	}
	defer rows.Close()

	subs := make([]automations.Subscriber, 0, len(inputs))
	i := 0
	for rows.Next() {
		var subID id.ID
		if err := rows.Scan(&subID); err != nil {
			return nil, fmt.Errorf("scan subscriber id: %w", err)
		}
		input := inputs[i]
		subs = append(subs, automations.Subscriber{
			ID:             subID,
			RuleID:         ruleID,
			SubscriberType: input.SubscriberType,
			ChannelID:      input.ChannelID,
			UserID:         input.UserID,
			RoleName:       input.RoleName,
			DocFieldPath:   input.DocFieldPath,
			DeliveryMethod: input.DeliveryMethod,
			Idx:            input.Idx,
		})
		i++
	}

	return subs, nil
}

// Ensure interface compliance
var _ automations.RuleRepository = (*AutomationRuleRepo)(nil)
