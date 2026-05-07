package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// OutboxStatus represents the state of an outbox message.
type OutboxStatus string

const (
	OutboxStatusPending    OutboxStatus = "pending"
	OutboxStatusProcessing OutboxStatus = "processing" // claimed by relay, Handle in progress
	OutboxStatusPublished  OutboxStatus = "published"
	OutboxStatusFailed     OutboxStatus = "failed"
)

// _defaultStuckTimeout is how long a message can stay in 'processing'
// before RecoverStuck resets it to 'pending'. Must be significantly
// longer than the longest adapter call (Telegram, email, webhook).
const _defaultStuckTimeout = 5 * time.Minute

// DefaultStuckTimeout returns the default timeout for stuck message recovery.
func DefaultStuckTimeout() time.Duration { return _defaultStuckTimeout }

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

// DomainEvent is imported from domain package.

// OutboxPublisher writes events to the outbox table.
type OutboxPublisher struct{}

// NewOutboxPublisher creates a new outbox publisher.
func NewOutboxPublisher() *OutboxPublisher {
	return &OutboxPublisher{}
}

// Publish writes an event to the outbox within the current transaction.
// MUST be called inside a transaction context.
func (p *OutboxPublisher) Publish(ctx context.Context, event domain.DomainEvent) error {
	txManager := MustGetTxManager(ctx)
	tx := txManager.GetTx(ctx)
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
func (p *OutboxPublisher) PublishBatch(ctx context.Context, events []domain.DomainEvent) (retErr error) {
	txManager := MustGetTxManager(ctx)
	tx := txManager.GetTx(ctx)
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
	defer func() {
		if cErr := results.Close(); cErr != nil && retErr == nil {
			retErr = fmt.Errorf("close batch results: %w", cErr)
		}
	}()

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

// ProcessBatch atomically claims pending messages, then processes them outside any transaction.
//
// Two-phase Atomic Claim Pattern:
//
//	Phase 1: CTE UPDATE RETURNING — atomically sets status='processing' in a single SQL statement.
//	         FOR UPDATE SKIP LOCKED inside the CTE prevents concurrent relay instances
//	         from claiming the same message.
//	Phase 2: Handle each message (may call external APIs: Telegram, email, webhook).
//	         Runs outside any transaction — no risk of long tx or statement_timeout.
//	         On success: mark 'published'. On failure: revert to 'pending' with retry.
//
// Returns number of successfully processed messages.
func (r *OutboxRelay) ProcessBatch(ctx context.Context) (int, error) {
	// Phase 1: Atomic claim.
	// Single SQL statement = implicit transaction = row locks held only for the UPDATE duration.
	rows, err := r.pool.Query(ctx, `
		WITH batch AS (
			SELECT id, created_at
			FROM sys_outbox
			WHERE status = $1
			  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE sys_outbox o
		SET status = $3
		FROM batch b
		WHERE o.id = b.id AND o.created_at = b.created_at
		RETURNING o.id, o.aggregate_type, o.aggregate_id, o.event_type, o.payload,
		          o.status, o.retry_count, o.last_error, o.next_retry_at, o.created_at, o.published_at
	`, OutboxStatusPending, r.batchSize, OutboxStatusProcessing)
	if err != nil {
		return 0, fmt.Errorf("claim outbox batch: %w", err)
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

	// Phase 2: Process each claimed message outside any transaction.
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
		// Revert to 'pending' (or 'failed' after max retries) so the message
		// can be picked up again on the next poll cycle.
		nextRetry := time.Now().Add(time.Duration(msg.RetryCount+1) * time.Minute)
		errStr := err.Error()

		_, updateErr := r.pool.Exec(ctx, `
			UPDATE sys_outbox 
			SET status = CASE WHEN retry_count >= 4 THEN $1 ELSE $2 END,
			    retry_count = retry_count + 1,
			    last_error = $3,
			    next_retry_at = $4
			WHERE id = $5 AND created_at = $6
		`, OutboxStatusFailed, OutboxStatusPending, errStr, nextRetry, msg.ID, msg.CreatedAt)

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
		WHERE id = $3 AND created_at = $4
	`, OutboxStatusPublished, now, msg.ID, msg.CreatedAt)

	return err
}

// RecoverStuck resets messages stuck in 'processing' (worker crash, OOM, etc.)
// back to 'pending' so they can be retried.
// Called periodically by the worker relay loop (e.g. every cleanup cycle).
func (r *OutboxRelay) RecoverStuck(ctx context.Context, timeout time.Duration) (int64, error) {
	cutoff := time.Now().Add(-timeout)
	result, err := r.pool.Exec(ctx, `
		UPDATE sys_outbox
		SET status = $1
		WHERE status = $2
		  AND created_at < $3
	`, OutboxStatusPending, OutboxStatusProcessing, cutoff)

	if err != nil {
		return 0, fmt.Errorf("recover stuck outbox messages: %w", err)
	}

	return result.RowsAffected(), nil
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
