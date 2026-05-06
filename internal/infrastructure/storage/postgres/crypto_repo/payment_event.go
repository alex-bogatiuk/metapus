package crypto_repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

const _paymentEventsTable = "reg_crypto_payment_events"

// PaymentEventRepo implements crypto.PaymentEventRepository.
type PaymentEventRepo struct {
	builder squirrel.StatementBuilderType
}

// NewPaymentEventRepo creates a new payment event repository.
func NewPaymentEventRepo() *PaymentEventRepo {
	return &PaymentEventRepo{
		builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// Create inserts a new payment event.
func (r *PaymentEventRepo) Create(ctx context.Context, event *crypto.PaymentEvent) error {
	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	q := r.builder.Insert(_paymentEventsTable).
		Columns("id", "payment_id", "from_status", "to_status", "event_type", "metadata", "created_at").
		Values(event.ID, event.PaymentID, event.FromStatus, event.ToStatus, event.EventType, metadataJSON, event.CreatedAt)

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	if _, err := querier.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert payment event: %w", err)
	}

	return nil
}

// GetByPaymentID returns all events for a payment ordered by creation time.
func (r *PaymentEventRepo) GetByPaymentID(ctx context.Context, paymentID id.ID) ([]crypto.PaymentEvent, error) {
	q := r.builder.Select(
		"id", "payment_id", "from_status", "to_status", "event_type", "metadata", "created_at",
	).From(_paymentEventsTable).
		Where(squirrel.Eq{"payment_id": paymentID}).
		OrderBy("created_at ASC")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var events []crypto.PaymentEvent
	if err := pgxscan.Select(ctx, querier, &events, sql, args...); err != nil {
		return nil, fmt.Errorf("select payment events: %w", err)
	}

	return events, nil
}
