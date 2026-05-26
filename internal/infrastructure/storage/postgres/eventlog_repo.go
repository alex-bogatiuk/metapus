package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/eventlog"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/cursor"
	"metapus/internal/infrastructure/storage/postgres/keyset"
)

// EventLogRepo implements eventlog.Writer and eventlog.Reader.
// It resolves the per-tenant TxManager from context at runtime (multi-tenant safe).
type EventLogRepo struct{}

// NewEventLogRepo creates a new event log repository.
func NewEventLogRepo() *EventLogRepo {
	return &EventLogRepo{}
}

// psql returns a squirrel builder with PostgreSQL dollar placeholders.
func (r *EventLogRepo) psql() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

// ---------------------------------------------------------------------------
// Writer
// ---------------------------------------------------------------------------

// Write records a single event. Enriches from context (trace, user).
// Uses TxManager from context (requires TenantDB middleware).
func (r *EventLogRepo) Write(ctx context.Context, event eventlog.Event) error {
	r.enrichFromContext(ctx, &event)
	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	return r.execInsert(ctx, querier, event)
}

// WriteDirect records a single event using the provided pool directly.
// Use in middleware that runs before TenantDB (Recovery, Logger) where TxManager is unavailable.
func (r *EventLogRepo) WriteDirect(ctx context.Context, pool *pgxpool.Pool, event eventlog.Event) error {
	r.enrichFromContext(ctx, &event)
	return r.execInsert(ctx, pool, event)
}

// execInsert performs the actual INSERT into sys_event_log via the given executor.
func (r *EventLogRepo) execInsert(ctx context.Context, exec interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}, event eventlog.Event) error {
	if id.IsNil(event.ID) {
		event.ID = id.New()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	detailsJSON, err := marshalDetails(event.Details)
	if err != nil {
		return fmt.Errorf("eventlog: marshal details: %w", err)
	}

	sql := `
		INSERT INTO sys_event_log (
			id, category, severity, event_type, source, session_id,
			user_id, client_ip,
			entity_type, entity_id, entity_number,
			message, details,
			trace_id, request_id, duration_ms,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8,
			$9, $10, $11,
			$12, $13,
			$14, $15, $16,
			$17
		)
	`

	_, err = exec.Exec(ctx, sql,
		event.ID, event.Category, event.Severity, event.EventType, event.Source, nilIfEmpty(event.SessionID),
		nilIfEmpty(event.UserID), parseIP(event.ClientIP),
		nilIfEmpty(event.EntityType), event.EntityID, nilIfEmpty(event.EntityNumber),
		event.Message, detailsJSON,
		nilIfEmpty(event.TraceID), nilIfEmpty(event.RequestID), event.DurationMs,
		event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("eventlog: insert: %w", err)
	}
	return nil
}

// WriteBatch records multiple events using pgx.Batch for performance.
func (r *EventLogRepo) WriteBatch(ctx context.Context, events []eventlog.Event) (retErr error) {
	if len(events) == 0 {
		return nil
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	b := &pgx.Batch{}

	sql := `
		INSERT INTO sys_event_log (
			id, category, severity, event_type, source, session_id,
			user_id, client_ip,
			entity_type, entity_id, entity_number,
			message, details,
			trace_id, request_id, duration_ms,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8,
			$9, $10, $11,
			$12, $13,
			$14, $15, $16,
			$17
		)
	`

	for i := range events {
		r.enrichFromContext(ctx, &events[i])
		event := events[i]

		if id.IsNil(event.ID) {
			event.ID = id.New()
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}

		detailsJSON, err := marshalDetails(event.Details)
		if err != nil {
			return fmt.Errorf("eventlog: marshal details batch[%d]: %w", i, err)
		}

		b.Queue(sql,
			event.ID, event.Category, event.Severity, event.EventType, event.Source, nilIfEmpty(event.SessionID),
			nilIfEmpty(event.UserID), parseIP(event.ClientIP),
			nilIfEmpty(event.EntityType), event.EntityID, nilIfEmpty(event.EntityNumber),
			event.Message, detailsJSON,
			nilIfEmpty(event.TraceID), nilIfEmpty(event.RequestID), event.DurationMs,
			event.CreatedAt,
		)
	}

	br := querier.SendBatch(ctx, b)
	defer func() {
		if cErr := br.Close(); cErr != nil && retErr == nil {
			retErr = fmt.Errorf("eventlog: close batch: %w", cErr)
		}
	}()

	for i := range events {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("eventlog: execute batch[%d]: %w", i, err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Reader
// ---------------------------------------------------------------------------

// Allowed sort columns for event log.
var eventLogOrderCols = map[string]struct{}{
	"created_at": {},
	"severity":   {},
	"category":   {},
	"event_type": {},
}

// Select columns for scanning.
var eventLogSelectCols = []string{
	"e.id", "e.category", "e.severity", "e.event_type",
	"e.source", "e.session_id",
	"e.user_id", "e.client_ip",
	"e.entity_type", "e.entity_id", "e.entity_number",
	"e.message", "e.details",
	"e.trace_id", "e.request_id", "e.duration_ms",
	"e.created_at",
	"COALESCE(u.email, e.user_id) AS user_email",
}

// List returns events matching the filter with cursor pagination.
func (r *EventLogRepo) List(ctx context.Context, f eventlog.Filter, cursorReq *cursor.Request) (eventlog.ListResult, error) {
	var result eventlog.ListResult

	spec, err := keyset.ParseOrderBy(f.OrderBy, "created_at", "DESC", eventLogOrderCols)
	if err != nil {
		return result, err
	}
	// Prefix sort field with table alias for JOIN query
	aliasedSpec := keyset.SortSpec{Field: "e." + spec.Field, Direction: spec.Direction, IDField: "e.id"}

	conditions := r.buildWhereConditions(f)
	querier := MustGetTxManager(ctx).GetQuerier(ctx)

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	// Dispatch based on cursor direction
	if cursorReq != nil {
		switch cursorReq.Direction {
		case cursor.DirAfter:
			return r.listForward(ctx, aliasedSpec, spec, conditions, cursorReq.Token, limit)
		case cursor.DirBefore:
			return r.listBackward(ctx, aliasedSpec, spec, conditions, cursorReq.Token, limit)
		}
	}

	// First page: count + fetch
	countQ := r.psql().Select("COUNT(*)").From("sys_event_log e")
	for _, cond := range conditions {
		countQ = countQ.Where(cond)
	}
	countSQL, countArgs, err := countQ.ToSql()
	if err != nil {
		return result, fmt.Errorf("eventlog: build count: %w", err)
	}
	if err := querier.QueryRow(ctx, countSQL, countArgs...).Scan(&result.TotalCount); err != nil {
		return result, fmt.Errorf("eventlog: count: %w", err)
	}

	q := r.psql().Select(eventLogSelectCols...).
		From("sys_event_log e").
		LeftJoin("users u ON u.id::text = e.user_id")
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.OrderBy(aliasedSpec.OrderByClause()).Limit(uint64(limit + 1))

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("eventlog: build list: %w", err)
	}

	items, err := r.scanEvents(ctx, querier, sqlStr, args)
	if err != nil {
		return result, err
	}

	if len(items) > limit {
		items = items[:limit]
		result.HasMore = true
	}
	result.HasPrev = false
	result.Items = items

	if len(result.Items) > 0 {
		r.setCursors(&result, spec)
	}
	return result, nil
}

// listForward fetches events after a cursor.
func (r *EventLogRepo) listForward(ctx context.Context, aliasedSpec, rawSpec keyset.SortSpec, conditions []squirrel.Sqlizer, token string, limit int) (eventlog.ListResult, error) {
	var result eventlog.ListResult

	values, err := keyset.DecodeCursor(token, rawSpec)
	if err != nil {
		return result, err
	}

	q := r.psql().Select(eventLogSelectCols...).
		From("sys_event_log e").
		LeftJoin("users u ON u.id::text = e.user_id")
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.Where(keyset.TupleCondition(aliasedSpec, values, true))
	q = q.OrderBy(aliasedSpec.OrderByClause()).Limit(uint64(limit + 1))

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("eventlog: build forward: %w", err)
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	items, err := r.scanEvents(ctx, querier, sqlStr, args)
	if err != nil {
		return result, err
	}

	if len(items) > limit {
		items = items[:limit]
		result.HasMore = true
	}
	result.HasPrev = true
	result.Items = items

	if len(result.Items) > 0 {
		r.setCursors(&result, rawSpec)
	}
	return result, nil
}

// listBackward fetches events before a cursor.
func (r *EventLogRepo) listBackward(ctx context.Context, aliasedSpec, rawSpec keyset.SortSpec, conditions []squirrel.Sqlizer, token string, limit int) (eventlog.ListResult, error) {
	var result eventlog.ListResult

	values, err := keyset.DecodeCursor(token, rawSpec)
	if err != nil {
		return result, err
	}

	q := r.psql().Select(eventLogSelectCols...).
		From("sys_event_log e").
		LeftJoin("users u ON u.id::text = e.user_id")
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.Where(keyset.TupleCondition(aliasedSpec, values, false))
	q = q.OrderBy(aliasedSpec.InvertedOrderByClause()).Limit(uint64(limit + 1))

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("eventlog: build backward: %w", err)
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	items, err := r.scanEvents(ctx, querier, sqlStr, args)
	if err != nil {
		return result, err
	}

	if len(items) > limit {
		items = items[:limit]
		result.HasPrev = true
	}
	result.HasMore = true

	// Reverse to restore original sort order
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	result.Items = items

	if len(result.Items) > 0 {
		r.setCursors(&result, rawSpec)
	}
	return result, nil
}

// GetByID returns a single event.
func (r *EventLogRepo) GetByID(ctx context.Context, eventID id.ID) (eventlog.Event, error) {
	q := r.psql().Select(eventLogSelectCols...).
		From("sys_event_log e").
		LeftJoin("users u ON u.id::text = e.user_id").
		Where(squirrel.Eq{"e.id": eventID}).
		Limit(1)

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return eventlog.Event{}, fmt.Errorf("eventlog: build getById: %w", err)
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	items, err := r.scanEvents(ctx, querier, sqlStr, args)
	if err != nil {
		return eventlog.Event{}, err
	}
	if len(items) == 0 {
		return eventlog.Event{}, fmt.Errorf("eventlog: event not found: %s", eventID)
	}
	return items[0], nil
}

// GetByTraceID returns all events sharing a trace ID, ordered chronologically.
func (r *EventLogRepo) GetByTraceID(ctx context.Context, traceID string) ([]eventlog.Event, error) {
	q := r.psql().Select(eventLogSelectCols...).
		From("sys_event_log e").
		LeftJoin("users u ON u.id::text = e.user_id").
		Where(squirrel.Eq{"e.trace_id": traceID}).
		OrderBy("e.created_at ASC")

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("eventlog: build trace query: %w", err)
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	return r.scanEvents(ctx, querier, sqlStr, args)
}

// GetStats returns aggregated counts by severity.
func (r *EventLogRepo) GetStats(ctx context.Context, f eventlog.StatsFilter) (eventlog.Stats, error) {
	var stats eventlog.Stats

	q := r.psql().
		Select(
			"COUNT(*) AS total",
			"COUNT(*) FILTER (WHERE severity = 'info') AS info",
			"COUNT(*) FILTER (WHERE severity = 'warning') AS warning",
			"COUNT(*) FILTER (WHERE severity = 'error') AS error",
			"COUNT(*) FILTER (WHERE severity = 'critical') AS critical",
		).
		From("sys_event_log")

	if f.DateFrom != nil {
		q = q.Where(squirrel.GtOrEq{"created_at": *f.DateFrom})
	}
	if f.DateTo != nil {
		q = q.Where(squirrel.LtOrEq{"created_at": *f.DateTo})
	}

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return stats, fmt.Errorf("eventlog: build stats: %w", err)
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	err = querier.QueryRow(ctx, sqlStr, args...).Scan(
		&stats.Total, &stats.Info, &stats.Warning, &stats.Error, &stats.Critical,
	)
	if err != nil {
		return stats, fmt.Errorf("eventlog: stats: %w", err)
	}
	return stats, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildWhereConditions builds WHERE clauses from the event log filter.
func (r *EventLogRepo) buildWhereConditions(f eventlog.Filter) []squirrel.Sqlizer {
	conds := make([]squirrel.Sqlizer, 0, 8)

	if len(f.Categories) > 0 {
		conds = append(conds, squirrel.Eq{"e.category": f.Categories})
	}
	if len(f.Severities) > 0 {
		conds = append(conds, squirrel.Eq{"e.severity": f.Severities})
	}
	if f.EventType != "" {
		conds = append(conds, squirrel.Eq{"e.event_type": f.EventType})
	}
	if f.UserID != "" {
		conds = append(conds, squirrel.Eq{"e.user_id": f.UserID})
	}
	if f.EntityType != "" {
		conds = append(conds, squirrel.Eq{"e.entity_type": f.EntityType})
	}
	if f.EntityID != nil {
		conds = append(conds, squirrel.Eq{"e.entity_id": *f.EntityID})
	}
	if f.EntityNumber != "" {
		conds = append(conds, squirrel.Eq{"e.entity_number": f.EntityNumber})
	}
	if f.Source != "" {
		conds = append(conds, squirrel.Eq{"e.source": f.Source})
	}
	if f.TraceID != "" {
		conds = append(conds, squirrel.Eq{"e.trace_id": f.TraceID})
	}
	if f.DateFrom != nil {
		conds = append(conds, squirrel.GtOrEq{"e.created_at": *f.DateFrom})
	}
	if f.DateTo != nil {
		conds = append(conds, squirrel.LtOrEq{"e.created_at": *f.DateTo})
	}
	if f.Search != "" {
		// Use ILIKE with trigram index for substring search
		conds = append(conds, squirrel.ILike{"e.message": "%" + f.Search + "%"})
	}

	return conds
}

// scanEvents scans event rows from a query result.
func (r *EventLogRepo) scanEvents(ctx context.Context, querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, sqlStr string, args []any) ([]eventlog.Event, error) {
	rows, err := querier.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("eventlog: query: %w", err)
	}
	defer rows.Close()

	items := make([]eventlog.Event, 0, 51)
	for rows.Next() {
		var e eventlog.Event
		var (
			sessionID    *string
			userID       *string
			clientIP     *net.IP
			entityType   *string
			entityID     *id.ID
			entityNumber *string
			details      []byte
			traceID      *string
			requestID    *string
			durationMs   *int
			userEmail    *string
		)

		err := rows.Scan(
			&e.ID, &e.Category, &e.Severity, &e.EventType,
			&e.Source, &sessionID,
			&userID, &clientIP,
			&entityType, &entityID, &entityNumber,
			&e.Message, &details,
			&traceID, &requestID, &durationMs,
			&e.CreatedAt,
			&userEmail,
		)
		if err != nil {
			return nil, fmt.Errorf("eventlog: scan: %w", err)
		}

		if sessionID != nil {
			e.SessionID = *sessionID
		}
		if userID != nil {
			e.UserID = *userID
		}
		if clientIP != nil {
			e.ClientIP = clientIP.String()
		}
		if entityType != nil {
			e.EntityType = *entityType
		}
		if entityID != nil {
			e.EntityID = entityID
		}
		if entityNumber != nil {
			e.EntityNumber = *entityNumber
		}
		if details != nil {
			_ = json.Unmarshal(details, &e.Details)
		}
		if traceID != nil {
			e.TraceID = *traceID
		}
		if requestID != nil {
			e.RequestID = *requestID
		}
		if durationMs != nil {
			e.DurationMs = durationMs
		}
		if userEmail != nil {
			e.UserEmail = *userEmail
		}

		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eventlog: rows: %w", err)
	}
	return items, nil
}

// setCursors builds prev/next cursor tokens from first/last items.
func (r *EventLogRepo) setCursors(result *eventlog.ListResult, spec keyset.SortSpec) {
	if len(result.Items) == 0 {
		return
	}
	first := result.Items[0]
	last := result.Items[len(result.Items)-1]

	// Resolve sort field value from event struct
	getSortValue := func(e eventlog.Event) any {
		switch spec.Field {
		case "created_at":
			return e.CreatedAt
		case "severity":
			return e.Severity
		case "category":
			return e.Category
		case "event_type":
			return e.EventType
		default:
			return e.CreatedAt
		}
	}

	if c, err := keyset.BuildCursorFromRow(spec, getSortValue(first), first.ID); err == nil {
		result.PrevCursor = c
	}
	if c, err := keyset.BuildCursorFromRow(spec, getSortValue(last), last.ID); err == nil {
		result.NextCursor = c
	}
}

// enrichFromContext populates trace/user info from request context.
func (r *EventLogRepo) enrichFromContext(ctx context.Context, event *eventlog.Event) {
	if trace := appctx.GetTrace(ctx); trace != nil {
		if event.TraceID == "" {
			event.TraceID = trace.TraceID
		}
		if event.RequestID == "" {
			event.RequestID = trace.RequestID
		}
	}
	if scope := security.GetScope(ctx); scope != nil {
		if event.UserID == "" {
			event.UserID = scope.UserID
		}
	}
	if event.Source == "" {
		event.Source = "api"
	}
}

// marshalDetails converts details map to JSON bytes, returns nil if empty.
func marshalDetails(details map[string]any) ([]byte, error) {
	if len(details) == 0 {
		return nil, nil
	}
	return json.Marshal(details)
}

// nilIfEmpty returns nil for empty strings (avoids storing empty strings in DB).
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// parseIP parses a string IP address, returns nil if empty or invalid.
func parseIP(s string) *net.IP {
	if s == "" {
		return nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil
	}
	return &ip
}
