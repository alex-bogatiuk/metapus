package postgres

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/automations"
)

// AutomationHistoryRepo implements automations.HistoryRepository
type AutomationHistoryRepo struct{}

func NewAutomationHistoryRepo() *AutomationHistoryRepo {
	return &AutomationHistoryRepo{}
}

func (r *AutomationHistoryRepo) Create(ctx context.Context, history *automations.ExecutionHistory) error {
	q := tenant.MustGetPool(ctx)

	history.ID = id.New()
	
	query, args, err := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar).
		Insert("sys_automation_history").
		Columns("id", "rule_id", "event_type", "aggregate_id", "success", "error_message", "request_payload").
		Values(history.ID, history.RuleID, history.EventType, history.AggregateID, history.Success, history.ErrorMessage, history.RequestPayload).
		ToSql()
	
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	_, err = q.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("exec insert: %w", err)
	}

	return nil
}

func (r *AutomationHistoryRepo) ListByRuleID(ctx context.Context, ruleID id.ID, limit int) ([]automations.ExecutionHistory, error) {
	q := tenant.MustGetPool(ctx)

	query, args, err := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar).
		Select("id", "rule_id", "event_type", "aggregate_id", "success", "error_message", "request_payload", "created_at").
		From("sys_automation_history").
		Where(squirrel.Eq{"rule_id": ruleID}).
		OrderBy("created_at DESC").
		Limit(uint64(limit)).
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()

	var result []automations.ExecutionHistory
	for rows.Next() {
		var h automations.ExecutionHistory
		if err := rows.Scan(&h.ID, &h.RuleID, &h.EventType, &h.AggregateID, &h.Success, &h.ErrorMessage, &h.RequestPayload, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		result = append(result, h)
	}

	return result, nil
}
