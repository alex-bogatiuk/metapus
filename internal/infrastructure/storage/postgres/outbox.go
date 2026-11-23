package postgres

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/workerjob"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	platformtelemetry "github.com/alex-bogatiuk/metapus/internal/platform/telemetry"
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
	Traceparent   string       `db:"traceparent"`
	CorrelationID string       `db:"correlation_id"`
	CausationID   string       `db:"causation_id"`
	Status        OutboxStatus `db:"status"`
	RetryCount    int          `db:"retry_count"`
	LastError     *string      `db:"last_error"`
	NextRetryAt   *time.Time   `db:"next_retry_at"`
	CreatedAt     time.Time    `db:"created_at"`
	PublishedAt   *time.Time   `db:"published_at"`
	ClaimToken    id.ID        `db:"claim_token"`
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

	business := platformtelemetry.BusinessIDs(ctx, event.AggregateID.String())
	_, err = tx.Exec(ctx, `
		INSERT INTO sys_outbox (id, aggregate_type, aggregate_id, event_type, payload, status, created_at,
		                        traceparent,correlation_id,causation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7,$8,$9,$10)
	`, id.New(), event.AggregateType, event.AggregateID, event.EventType, payloadBytes, OutboxStatusPending,
		time.Now().UTC(), platformtelemetry.Traceparent(ctx), business.CorrelationID, business.CausationID)

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

		business := platformtelemetry.BusinessIDs(ctx, event.AggregateID.String())
		batch.Queue(`
			INSERT INTO sys_outbox (id, aggregate_type, aggregate_id, event_type, payload, status, created_at,
			                        traceparent,correlation_id,causation_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7,$8,$9,$10)
		`, id.New(), event.AggregateType, event.AggregateID, event.EventType, payloadBytes, OutboxStatusPending,
			now, platformtelemetry.Traceparent(ctx), business.CorrelationID, business.CausationID)
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
	pool                  *pgxpool.Pool
	batchSize             int
	handler               OutboxHandler
	aggregateTypes        []string
	excludeAggregateTypes []string
	slots                 chan struct{}
	backoff               BackoffStrategy
}

// OutboxHandler processes outbox messages.
type OutboxHandler interface {
	// Handle processes a message and returns error if failed
	Handle(ctx context.Context, msg *OutboxMessage) error
}

// OutboxBackoffProvider optionally selects a retry schedule per message.
type OutboxBackoffProvider interface {
	BackoffFor(msg *OutboxMessage) BackoffStrategy
}

// RetryPolicy controls retries for one outbox message without imposing
// webhook-specific limits on unrelated outbox consumers.
type RetryPolicy struct {
	Backoff     BackoffStrategy
	MaxAttempts int
	Deadline    time.Time
}

type OutboxRetryPolicyProvider interface {
	RetryPolicyFor(msg *OutboxMessage) RetryPolicy
}

// BackoffStrategy computes the next retry delay based on the current retry count (0-indexed).
// Implementations must be stateless — the retry count is the only input.
type BackoffStrategy interface {
	// NextDelay returns the delay before the (retryCount+1)-th retry attempt.
	// retryCount is 0-based: 0 = first failure, 1 = second failure, etc.
	NextDelay(retryCount int) time.Duration
}

// LinearBackoff implements BackoffStrategy with linear intervals: (retryCount+1) * interval.
// It is the default strategy of every outbox handler.
type LinearBackoff struct {
	Interval time.Duration
}

// NextDelay returns (retryCount+1) * interval.
func (b LinearBackoff) NextDelay(retryCount int) time.Duration {
	return time.Duration(retryCount+1) * b.Interval
}

// _defaultLinearBackoff is the default backoff for all outbox handlers.
var _defaultLinearBackoff BackoffStrategy = LinearBackoff{Interval: time.Minute}

// TableBackoff implements BackoffStrategy with explicit per-attempt intervals.
// If retryCount exceeds the table length, the last interval is reused.
type TableBackoff struct {
	Intervals []time.Duration
}

// DeterministicJitterBackoff spreads retries while keeping the schedule stable
// for a message across worker restarts.
type DeterministicJitterBackoff struct {
	Base     BackoffStrategy
	Seed     string
	Fraction float64
}

func (b DeterministicJitterBackoff) NextDelay(retryCount int) time.Duration {
	if b.Base == nil {
		return time.Minute
	}
	base := b.Base.NextDelay(retryCount)
	if b.Fraction <= 0 {
		return base
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(b.Seed))
	_, _ = h.Write([]byte(":" + strconv.Itoa(retryCount)))
	unit := (float64(h.Sum64()%20001) / 10000.0) - 1.0
	return time.Duration(float64(base) * (1 + unit*b.Fraction))
}

// NextDelay returns the interval for the given retry count.
// Falls back to the last interval if retryCount >= len(Intervals).
func (b TableBackoff) NextDelay(retryCount int) time.Duration {
	if retryCount < len(b.Intervals) {
		return b.Intervals[retryCount]
	}
	if len(b.Intervals) > 0 {
		return b.Intervals[len(b.Intervals)-1]
	}
	return time.Minute // safety fallback
}

// WebhookBackoff returns interval deltas that place attempts at approximately
// 0, 30s, 2m, 10m, 1h, 6h, 12h, 24h, 48h and 72h from event creation.
func WebhookBackoff() BackoffStrategy {
	return TableBackoff{
		Intervals: []time.Duration{
			30 * time.Second,
			90 * time.Second,
			8 * time.Minute,
			50 * time.Minute,
			5 * time.Hour,
			6 * time.Hour,
			12 * time.Hour,
			24 * time.Hour,
			24 * time.Hour,
		},
	}
}

func WebhookRetryPolicy(msg *OutboxMessage) RetryPolicy {
	return RetryPolicy{
		Backoff: DeterministicJitterBackoff{
			Base: WebhookBackoff(), Seed: msg.ID.String(), Fraction: 0.10,
		},
		MaxAttempts: 10,
		Deadline:    msg.CreatedAt.Add(72 * time.Hour),
	}
}

// NewOutboxRelay creates a new outbox relay with the default linear backoff.
func NewOutboxRelay(pool *pgxpool.Pool, batchSize int, handler OutboxHandler) *OutboxRelay {
	return &OutboxRelay{
		pool:      pool,
		batchSize: max(1, min(batchSize, 100)),
		handler:   handler,
		backoff:   _defaultLinearBackoff,
		slots:     make(chan struct{}, max(1, min(batchSize, 8))),
	}
}

// NewOutboxRelayWithBackoff creates a new outbox relay with a custom backoff strategy.
func NewOutboxRelayWithBackoff(pool *pgxpool.Pool, batchSize int, handler OutboxHandler, backoff BackoffStrategy) *OutboxRelay {
	return &OutboxRelay{
		pool:      pool,
		batchSize: max(1, min(batchSize, 100)),
		handler:   handler,
		backoff:   backoff,
		slots:     make(chan struct{}, max(1, min(batchSize, 8))),
	}
}

// FilterAggregates reserves a separate consumer for a class of events.
func (r *OutboxRelay) FilterAggregates(include, exclude []string) *OutboxRelay {
	r.aggregateTypes, r.excludeAggregateTypes = include, exclude
	return r
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
			  AND (next_retry_at IS NULL OR next_retry_at <= clock_timestamp())
			  AND ($4::text[] IS NULL OR aggregate_type = ANY($4))
			  AND ($5::text[] IS NULL OR NOT aggregate_type = ANY($5))
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE sys_outbox o
		SET status = $3, claim_token=gen_random_uuid_v7(), claimed_at=clock_timestamp(),
                lease_until=clock_timestamp()+INTERVAL '5 minutes'
		FROM batch b
		WHERE o.id = b.id AND o.created_at = b.created_at
		RETURNING o.id, o.aggregate_type, o.aggregate_id, o.event_type, o.payload,
		          o.traceparent,o.correlation_id,o.causation_id,
		          o.status, o.retry_count, o.last_error, o.next_retry_at, o.created_at, o.published_at, o.claim_token
	`, OutboxStatusPending, r.batchSize, OutboxStatusProcessing, r.aggregateTypes, r.excludeAggregateTypes)
	if err != nil {
		return 0, fmt.Errorf("claim outbox batch: %w", err)
	}
	defer rows.Close()

	var messages []*OutboxMessage
	for rows.Next() {
		var msg OutboxMessage
		err := rows.Scan(
			&msg.ID, &msg.AggregateType, &msg.AggregateID, &msg.EventType,
			&msg.Payload, &msg.Traceparent, &msg.CorrelationID, &msg.CausationID,
			&msg.Status, &msg.RetryCount, &msg.LastError,
			&msg.NextRetryAt, &msg.CreatedAt, &msg.PublishedAt, &msg.ClaimToken,
		)
		if err != nil {
			return 0, fmt.Errorf("scan outbox message: %w", err)
		}
		messages = append(messages, &msg)
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate outbox messages: %w", err)
	}

	// Every claimed task starts lease renewal before waiting for a handler slot.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var processingErrors []error
	processed := 0
	for _, msg := range messages {
		wg.Go(func() {
			err := r.processMessage(ctx, msg)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				processingErrors = append(processingErrors, err)
			} else {
				processed++
			}
		})
	}
	wg.Wait()
	return processed, errors.Join(processingErrors...)
}

// processMessage handles a single outbox message.
func (r *OutboxRelay) processMessage(ctx context.Context, msg *OutboxMessage) error {
	if msg.CorrelationID == "" {
		msg.CorrelationID = msg.AggregateID.String()
	}
	if msg.CausationID == "" {
		msg.CausationID = msg.AggregateID.String()
	}
	workCtx := platformtelemetry.ContinueTrace(ctx, msg.Traceparent)
	workCtx = platformtelemetry.WithBusinessContext(workCtx, msg.CorrelationID, msg.CausationID)
	workCtx, span := otel.Tracer("github.com/alex-bogatiuk/metapus/outbox").Start(workCtx, "outbox.deliver",
		oteltrace.WithSpanKind(oteltrace.SpanKindConsumer), oteltrace.WithAttributes(
			attribute.String("outbox.id", msg.ID.String()),
			attribute.String("outbox.event_type", msg.EventType),
			attribute.String("business.correlation_id", msg.CorrelationID),
			attribute.String("business.causation_id", msg.CausationID),
		))
	defer span.End()
	err := workerjob.WithLease(workCtx, time.Minute, func(renewCtx context.Context) error {
		result, renewErr := r.pool.Exec(renewCtx, `UPDATE sys_outbox
   SET lease_until=clock_timestamp()+INTERVAL '5 minutes'
   WHERE id=$1 AND created_at=$2 AND status='processing' AND claim_token=$3 AND lease_until>clock_timestamp()`, msg.ID, msg.CreatedAt, msg.ClaimToken)
		if renewErr != nil {
			return renewErr
		}
		if result.RowsAffected() != 1 {
			return workerjob.ErrLeaseLost
		}
		return nil
	}, func(handlerCtx context.Context) error {
		select {
		case r.slots <- struct{}{}:
		case <-handlerCtx.Done():
			return handlerCtx.Err()
		}
		defer func() { <-r.slots }()
		return r.handler.Handle(handlerCtx, msg)
	})
	if errors.Is(err, workerjob.ErrLeaseLost) || ctx.Err() != nil {
		return err
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "outbox delivery failed")
	}

	if err != nil {
		// Revert to 'pending' (or 'failed' after max retries) so the message
		// can be picked up again on the next poll cycle.
		policy := RetryPolicy{Backoff: r.backoff, MaxAttempts: 5}
		if provider, ok := r.handler.(OutboxRetryPolicyProvider); ok {
			selected := provider.RetryPolicyFor(msg)
			if selected.Backoff != nil {
				policy.Backoff = selected.Backoff
			}
			if selected.MaxAttempts > 0 {
				policy.MaxAttempts = selected.MaxAttempts
			}
			policy.Deadline = selected.Deadline
		} else if provider, ok := r.handler.(OutboxBackoffProvider); ok {
			if selected := provider.BackoffFor(msg); selected != nil {
				policy.Backoff = selected
			}
		}
		now := time.Now().UTC()
		nextRetry := now.Add(policy.Backoff.NextDelay(msg.RetryCount))
		terminal := msg.RetryCount >= policy.MaxAttempts-1
		if !policy.Deadline.IsZero() {
			if !now.Before(policy.Deadline) {
				terminal = true
			} else if nextRetry.After(policy.Deadline) {
				nextRetry = policy.Deadline
			}
		}
		errStr := err.Error()

		result, updateErr := r.pool.Exec(ctx, `
			UPDATE sys_outbox 
			SET status = CASE WHEN $1 THEN $2::outbox_status ELSE $3::outbox_status END,
			    retry_count = retry_count + 1,
			    last_error = $4,
			    next_retry_at = CASE WHEN $1 THEN NULL ELSE $5::timestamptz END,
                claim_token=NULL, claimed_at=NULL, lease_until=NULL
			WHERE id = $6 AND created_at = $7 AND status='processing' AND claim_token=$8 AND lease_until>clock_timestamp()
		`, terminal, OutboxStatusFailed, OutboxStatusPending, errStr, nextRetry, msg.ID, msg.CreatedAt, msg.ClaimToken)

		if updateErr != nil {
			return fmt.Errorf("update failed message: %w", updateErr)
		}
		if result.RowsAffected() != 1 {
			return errors.Join(err, workerjob.ErrLeaseLost)
		}
		return err
	}

	// Mark as published
	now := time.Now().UTC()
	result, err := r.pool.Exec(ctx, `
		UPDATE sys_outbox 
		SET status = $1, published_at = $2, claim_token=NULL, claimed_at=NULL, lease_until=NULL
		WHERE id = $3 AND created_at = $4 AND status='processing' AND claim_token=$5 AND lease_until>clock_timestamp()
	`, OutboxStatusPublished, now, msg.ID, msg.CreatedAt, msg.ClaimToken)

	if err == nil && result.RowsAffected() != 1 {
		return workerjob.ErrLeaseLost
	}
	return err
}

// RecoverStuck resets messages stuck in 'processing' (worker crash, OOM, etc.)
// back to 'pending' so they can be retried.
// Called periodically by the worker relay loop (e.g. every cleanup cycle).
func (r *OutboxRelay) RecoverStuck(ctx context.Context, timeout time.Duration) (int64, error) {
	_ = timeout // Compatibility: expiry is determined by the persisted lease.
	result, err := r.pool.Exec(ctx, `
		UPDATE sys_outbox
		SET status = $1, claim_token=NULL, claimed_at=NULL, lease_until=NULL
		WHERE status = $2
		  AND lease_until <= clock_timestamp()
	`, OutboxStatusPending, OutboxStatusProcessing)

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
		INSERT INTO sys_outbox_dlq (
			id,aggregate_type,aggregate_id,event_type,payload,retry_count,last_error,
			created_at,failed_at,failure_reason,traceparent,correlation_id,causation_id)
		SELECT id,aggregate_type,aggregate_id,event_type,payload,retry_count,last_error,
		       created_at,NOW(),last_error,traceparent,correlation_id,causation_id
		FROM moved
	`, OutboxStatusFailed)

	if err != nil {
		return 0, fmt.Errorf("move to DLQ: %w", err)
	}

	return result.RowsAffected(), nil
}
