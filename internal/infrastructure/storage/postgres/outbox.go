package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	
	"metapus/internal/core/id"
)

// OutboxStatus represents the state of an outbox message.
type OutboxStatus string

const (
	OutboxStatusPending   OutboxStatus = "pending"
	OutboxStatusPublished OutboxStatus = "published"
	OutboxStatusFailed    OutboxStatus = "failed"
)

// OutboxMessage represents a message in the transactional outbox.
type OutboxMessage struct {
	ID            id.ID        `db:"id"`
	AggregateType string       `db:"aggregate_type"` // e.g., "Invoice", "Counterparty"
	AggregateID   id.ID        `db:"aggregate_id"`   // ID of the entity
	EventType     string       `db:"event_type"`     // e.g., "InvoicePosted", "CounterpartyCreated"
	Payload       []byte       `db:"payload"`        // JSON payload
	Status        OutboxStatus `db:"status"`
	RetryCount    int          `db:"retry_count"`
	LastError     *string      `db:"last_error"`
	NextRetryAt   *time.Time   `db:"next_retry_at"`
	CreatedAt     time.Time    `db:"created_at"`
	PublishedAt   *time.Time   `db:"published_at"`
}

// DomainEvent represents an event to be published via outbox.
type DomainEvent struct {
	AggregateType string
	AggregateID   id.ID
	EventType     string
	Payload       any
}

// OutboxPublisher writes events to the outbox table.
type OutboxPublisher struct {
	txManager *TxManager
}

// NewOutboxPublisher creates a new outbox publisher.
func NewOutboxPublisher(txManager *TxManager) *OutboxPublisher {
	return &OutboxPublisher{txManager: txManager}
}

// Publish writes an event to the outbox within the current transaction.
// MUST be called inside a transaction context.
func (p *OutboxPublisher) Publish(ctx context.Context, event DomainEvent) error {
	tx := p.txManager.GetTx(ctx)
	if tx == nil {
		return fmt.Errorf("outbox publish requires transaction context")
	}
	
	payloadBytes, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	
	_, err = tx.Exec(ctx, `
		INSERT INTO sys_outbox (id, aggregate_type, aggregate_id, event_type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id.New(), event.AggregateType, event.AggregateID, event.EventType, payloadBytes, OutboxStatusPending, time.Now().UTC())
	
	if err != nil {
		return fmt.Errorf("insert outbox message: %w", err)
	}
	
	return nil
}

// PublishBatch writes multiple events to the outbox.
func (p *OutboxPublisher) PublishBatch(ctx context.Context, events []DomainEvent) error {
	tx := p.txManager.GetTx(ctx)
	if tx == nil {
		return fmt.Errorf("outbox publish requires transaction context")
	}
	
	batch := &pgx.Batch{}
	now := time.Now().UTC()
	
	for _, event := range events {
		payloadBytes, err := json.Marshal(event.Payload)
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
		
		batch.Queue(`
			INSERT INTO sys_outbox (id, aggregate_type, aggregate_id, event_type, payload, status, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, id.New(), event.AggregateType, event.AggregateID, event.EventType, payloadBytes, OutboxStatusPending, now)
	}
	
	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	
	for range events {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch insert outbox message: %w", err)
		}
	}
	
	return nil
}

// OutboxRelay reads and processes messages from the outbox.
// Used by the background worker to publish events to message broker.
type OutboxRelay struct {
	pool      *pgxpool.Pool
	batchSize int
	handler   OutboxHandler
}

// OutboxHandler processes outbox messages.
type OutboxHandler interface {
	// Handle processes a message and returns error if failed
	Handle(ctx context.Context, msg *OutboxMessage) error
}

// NewOutboxRelay creates a new outbox relay.
func NewOutboxRelay(pool *pgxpool.Pool, batchSize int, handler OutboxHandler) *OutboxRelay {
	return &OutboxRelay{
		pool:      pool,
		batchSize: batchSize,
		handler:   handler,
	}
}

// ProcessBatch fetches and processes pending messages.
// Returns number of processed messages.
func (r *OutboxRelay) ProcessBatch(ctx context.Context) (int, error) {
	// Fetch pending messages with lock to prevent concurrent processing
	rows, err := r.pool.Query(ctx, `
		SELECT id, aggregate_type, aggregate_id, event_type, payload, status, 
		       retry_count, last_error, next_retry_at, created_at, published_at
		FROM sys_outbox
		WHERE status = $1 
		  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		ORDER BY created_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, OutboxStatusPending, r.batchSize)
	if err != nil {
		return 0, fmt.Errorf("fetch outbox messages: %w", err)
	}
	defer rows.Close()
	
	var messages []*OutboxMessage
	for rows.Next() {
		var msg OutboxMessage
		err := rows.Scan(
			&msg.ID, &msg.AggregateType, &msg.AggregateID, &msg.EventType,
			&msg.Payload, &msg.Status, &msg.RetryCount, &msg.LastError,
			&msg.NextRetryAt, &msg.CreatedAt, &msg.PublishedAt,
		)
		if err != nil {
			return 0, fmt.Errorf("scan outbox message: %w", err)
		}
		messages = append(messages, &msg)
	}
	
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate outbox messages: %w", err)
	}
	
	processed := 0
	for _, msg := range messages {
		if err := r.processMessage(ctx, msg); err != nil {
			// Log but continue processing other messages
			continue
		}
		processed++
	}
	
	return processed, nil
}

// processMessage handles a single outbox message.
func (r *OutboxRelay) processMessage(ctx context.Context, msg *OutboxMessage) error {
	err := r.handler.Handle(ctx, msg)
	
	if err != nil {
		// Increment retry count and set next retry time (exponential backoff)
		nextRetry := time.Now().Add(time.Duration(msg.RetryCount+1) * time.Minute)
		errStr := err.Error()
		
		_, updateErr := r.pool.Exec(ctx, `
			UPDATE sys_outbox 
			SET retry_count = retry_count + 1,
			    last_error = $1,
			    next_retry_at = $2,
			    status = CASE WHEN retry_count >= 5 THEN $3 ELSE status END
			WHERE id = $4
		`, errStr, nextRetry, OutboxStatusFailed, msg.ID)
		
		if updateErr != nil {
			return fmt.Errorf("update failed message: %w", updateErr)
		}
		return err
	}
	
	// Mark as published
	now := time.Now().UTC()
	_, err = r.pool.Exec(ctx, `
		UPDATE sys_outbox 
		SET status = $1, published_at = $2
		WHERE id = $3
	`, OutboxStatusPublished, now, msg.ID)
	
	return err
}

// MoveToDLQ moves failed messages to dead letter queue.
func (r *OutboxRelay) MoveToDLQ(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `
		WITH moved AS (
			DELETE FROM sys_outbox
			WHERE status = $1 AND retry_count >= 5
			RETURNING *
		)
		INSERT INTO sys_outbox_dlq 
		SELECT *, NOW() as failed_at, last_error as failure_reason FROM moved
	`, OutboxStatusFailed)
	
	if err != nil {
		return 0, fmt.Errorf("move to DLQ: %w", err)
	}
	
	return result.RowsAffected(), nil
}
