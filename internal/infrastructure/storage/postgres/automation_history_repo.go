package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationHistoryRepo implements automations.HistoryRepository (append-only).
type AutomationHistoryRepo struct{}

// NewAutomationHistoryRepo creates a new repository.
func NewAutomationHistoryRepo() *AutomationHistoryRepo {
	return &AutomationHistoryRepo{}
}

const historySelectCols = `id, rule_id, rule_name, event_type, aggregate_id, aggregate_name,
	status, channel_id, channel_name, account_name,
	rendered_payload, error_text, duration_ms, created_at`

// Create saves a new history entry.
func (r *AutomationHistoryRepo) Create(ctx context.Context, entry *automations.HistoryEntry) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	entry.ID = id.New()

	query := `
		INSERT INTO sys_automation_history (
			id, rule_id, rule_name, event_type, aggregate_id, aggregate_name,
			status, channel_id, channel_name, account_name,
			rendered_payload, error_text, duration_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err := q.Exec(ctx, query,
		entry.ID, entry.RuleID, entry.RuleName, entry.EventType, entry.AggregateID, entry.AggregateName,
		entry.Status, entry.ChannelID, entry.ChannelName, entry.AccountName,
		entry.RenderedPayload, entry.ErrorText, entry.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("insert history: %w", err)
	}

	return nil
}

// List returns filtered and paginated history entries.
func (r *AutomationHistoryRepo) List(ctx context.Context, filter automations.HistoryFilter) ([]automations.HistoryEntry, int, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	where, args := buildHistoryWhere(filter)

	// Count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM sys_automation_history %s", where)
	var total int
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count history: %w", err)
	}

	// Data query
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := max(filter.Offset, 0)

	dataQuery := fmt.Sprintf(`
		SELECT %s
		FROM sys_automation_history %s
		ORDER BY created_at DESC
		LIMIT %d OFFSET %d
	`, historySelectCols, where, limit, offset)

	rows, err := q.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var entries []automations.HistoryEntry
	for rows.Next() {
		e, scanErr := scanHistoryRow(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("scan history: %w", scanErr)
		}
		entries = append(entries, *e)
	}

	return entries, total, nil
}

// GetByID retrieves a single history entry.
func (r *AutomationHistoryRepo) GetByID(ctx context.Context, entryID id.ID) (*automations.HistoryEntry, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`SELECT %s FROM sys_automation_history WHERE id = $1`, historySelectCols)

	var e automations.HistoryEntry
	err := q.QueryRow(ctx, query, entryID).Scan(
		&e.ID, &e.RuleID, &e.RuleName, &e.EventType, &e.AggregateID, &e.AggregateName,
		&e.Status, &e.ChannelID, &e.ChannelName, &e.AccountName,
		&e.RenderedPayload, &e.ErrorText, &e.DurationMs, &e.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_automation_history", entryID)
		}
		return nil, fmt.Errorf("get history by id: %w", err)
	}

	return &e, nil
}

// scanHistoryRow scans a pgx.Rows row into a HistoryEntry.
func scanHistoryRow(rows pgx.Rows) (*automations.HistoryEntry, error) {
	var e automations.HistoryEntry
	err := rows.Scan(
		&e.ID, &e.RuleID, &e.RuleName, &e.EventType, &e.AggregateID, &e.AggregateName,
		&e.Status, &e.ChannelID, &e.ChannelName, &e.AccountName,
		&e.RenderedPayload, &e.ErrorText, &e.DurationMs, &e.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// buildHistoryWhere builds shared WHERE clause and args from HistoryFilter.
func buildHistoryWhere(filter automations.HistoryFilter) (string, []any) {
	where := "WHERE 1=1"
	args := []any{}
	argIdx := 1

	if filter.RuleID != nil {
		where += fmt.Sprintf(" AND rule_id = $%d", argIdx)
		args = append(args, *filter.RuleID)
		argIdx++
	}
	if filter.Status != nil {
		where += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, *filter.Status)
		argIdx++
	}
	if filter.ChannelID != nil {
		where += fmt.Sprintf(" AND channel_id = $%d", argIdx)
		args = append(args, *filter.ChannelID)
		argIdx++
	}
	if filter.From != nil {
		where += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *filter.From)
		argIdx++
	}
	if filter.To != nil {
		where += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *filter.To)
	}

	return where, args
}

// CountByStatus returns aggregated counts grouped by status.
func (r *AutomationHistoryRepo) CountByStatus(ctx context.Context, filter automations.HistoryFilter) (*automations.HistoryStats, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	// For stats we ignore Status filter — we want ALL statuses counted
	filterNoStatus := filter
	filterNoStatus.Status = nil
	where, args := buildHistoryWhere(filterNoStatus)

	query := fmt.Sprintf(`SELECT status, COUNT(*) FROM sys_automation_history %s GROUP BY status`, where)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("count history by status: %w", err)
	}
	defer rows.Close()

	stats := &automations.HistoryStats{
		ByStatus: make(map[automations.HistoryStatus]int),
	}
	for rows.Next() {
		var status automations.HistoryStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan history stats: %w", err)
		}
		stats.ByStatus[status] = count
		stats.Total += count
	}

	return stats, nil
}

// ListIDsByStatus returns IDs of entries matching the filter. Used for batch replay.
func (r *AutomationHistoryRepo) ListIDsByStatus(ctx context.Context, filter automations.HistoryFilter, limit int) ([]id.ID, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	where, args := buildHistoryWhere(filter)

	if limit <= 0 {
		limit = 200
	}

	query := fmt.Sprintf(`SELECT id FROM sys_automation_history %s ORDER BY created_at DESC LIMIT %d`, where, limit)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list history ids: %w", err)
	}
	defer rows.Close()

	var ids []id.ID
	for rows.Next() {
		var entryID id.ID
		if err := rows.Scan(&entryID); err != nil {
			return nil, fmt.Errorf("scan history id: %w", err)
		}
		ids = append(ids, entryID)
	}

	return ids, nil
}

// Ensure interface compliance
var _ automations.HistoryRepository = (*AutomationHistoryRepo)(nil)
