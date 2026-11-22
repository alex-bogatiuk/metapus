package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
)

// OutboxRecord is an outbox message whose payload and trace context the
// caller has already fixed, such as the delivery of an immutable event it
// stored itself. OutboxPublisher.Publish derives them from a DomainEvent
// and the request instead.
type OutboxRecord struct {
	// ID is the message ID; a zero ID gets a new one.
	ID            id.ID
	AggregateType string
	AggregateID   id.ID
	EventType     string
	// Payload is the JSON object the message delivers, as stored.
	Payload []byte
	// CreatedAt is when the message was written; zero means now.
	CreatedAt     time.Time
	Traceparent   string
	CorrelationID string
	CausationID   string
}

// EnqueueOutbox writes pending messages through the current querier — the
// transaction when ctx carries one, so they commit with the caller's state.
// Several records are written in one batch, all or none.
func EnqueueOutbox(ctx context.Context, records ...OutboxRecord) (retErr error) {
	if len(records) == 0 {
		return nil
	}
	now := time.Now().UTC()
	batch := &pgx.Batch{}
	for _, r := range records {
		if id.IsNil(r.ID) {
			r.ID = id.New()
		}
		if r.CreatedAt.IsZero() {
			r.CreatedAt = now
		}
		batch.Queue(`
			INSERT INTO sys_outbox (id, aggregate_type, aggregate_id, event_type, payload, status, created_at,
			                        traceparent, correlation_id, causation_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, r.ID, r.AggregateType, r.AggregateID, r.EventType, r.Payload, OutboxStatusPending, r.CreatedAt,
			r.Traceparent, r.CorrelationID, r.CausationID)
	}
	results := MustGetTxManager(ctx).GetQuerier(ctx).SendBatch(ctx, batch)
	defer func() {
		if err := results.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close outbox batch: %w", err)
		}
	}()
	for range records {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("insert outbox message: %w", err)
		}
	}
	return nil
}

// PendingOutboxPayloadValues lists, for messages of the aggregate type not
// yet delivered (pending or being processed), the text of the payload's
// top-level field, so a caller that re-enqueues lost messages can skip the
// ones still on their way.
func PendingOutboxPayloadValues(ctx context.Context, aggregateType, field string) ([]string, error) {
	rows, err := MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT DISTINCT payload->>$2 FROM sys_outbox
		WHERE aggregate_type = $1 AND status IN ('pending', 'processing') AND payload->>$2 IS NOT NULL
	`, aggregateType, field)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox messages: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan pending outbox message: %w", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending outbox messages: %w", err)
	}
	return out, nil
}
