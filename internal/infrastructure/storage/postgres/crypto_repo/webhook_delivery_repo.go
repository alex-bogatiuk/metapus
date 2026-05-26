package crypto_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

const _webhookDeliveriesTable = "sys_webhook_deliveries"

// WebhookDeliveryRepo implements crypto.WebhookDeliveryRepository.
type WebhookDeliveryRepo struct {
	builder squirrel.StatementBuilderType
}

// NewWebhookDeliveryRepo creates a new webhook delivery repository.
func NewWebhookDeliveryRepo() *WebhookDeliveryRepo {
	return &WebhookDeliveryRepo{
		builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// Create inserts a new webhook delivery record.
func (r *WebhookDeliveryRepo) Create(ctx context.Context, delivery *crypto.WebhookDelivery) error {
	q := r.builder.Insert(_webhookDeliveriesTable).
		Columns(
			"id", "invoice_id", "merchant_id", "event_type",
			"webhook_url", "delivery_id", "status_code",
			"response_time_ms", "attempt", "error_message",
			"request_body", "created_at",
		).
		Values(
			delivery.ID, delivery.InvoiceID, delivery.MerchantID, delivery.EventType,
			delivery.WebhookURL, delivery.DeliveryID, delivery.StatusCode,
			delivery.ResponseTimeMs, delivery.Attempt, delivery.ErrorMessage,
			delivery.RequestBody, delivery.CreatedAt,
		)

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	if _, err := querier.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert webhook delivery: %w", err)
	}

	return nil
}

// ListByInvoice returns delivery records for a specific invoice, ordered by created_at DESC.
func (r *WebhookDeliveryRepo) ListByInvoice(ctx context.Context, invoiceID id.ID, limit, offset int) ([]crypto.WebhookDelivery, int, error) {
	return r.listByColumn(ctx, "invoice_id", invoiceID, limit, offset)
}

// ListByMerchant returns delivery records for a merchant, ordered by created_at DESC.
func (r *WebhookDeliveryRepo) ListByMerchant(ctx context.Context, merchantID id.ID, limit, offset int) ([]crypto.WebhookDelivery, int, error) {
	return r.listByColumn(ctx, "merchant_id", merchantID, limit, offset)
}

// listByColumn is a generic list query filtered by a single UUID column.
func (r *WebhookDeliveryRepo) listByColumn(ctx context.Context, column string, value id.ID, limit, offset int) ([]crypto.WebhookDelivery, int, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	// Count total
	countQ := r.builder.Select("COUNT(*)").
		From(_webhookDeliveriesTable).
		Where(squirrel.Eq{column: value})

	countSQL, countArgs, err := countQ.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("build count query: %w", err)
	}

	var total int
	if err := querier.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count webhook deliveries: %w", err)
	}

	if total == 0 {
		return []crypto.WebhookDelivery{}, 0, nil
	}

	// Fetch page
	dataQ := r.builder.Select(
		"id", "invoice_id", "merchant_id", "event_type",
		"webhook_url", "delivery_id", "status_code",
		"response_time_ms", "attempt", "error_message",
		"request_body", "created_at",
	).
		From(_webhookDeliveriesTable).
		Where(squirrel.Eq{column: value}).
		OrderBy("created_at DESC").
		Limit(uint64(limit)).
		Offset(uint64(offset))

	dataSQL, dataArgs, err := dataQ.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("build data query: %w", err)
	}

	var deliveries []crypto.WebhookDelivery
	if err := pgxscan.Select(ctx, querier, &deliveries, dataSQL, dataArgs...); err != nil {
		return nil, 0, fmt.Errorf("select webhook deliveries: %w", err)
	}

	return deliveries, total, nil
}

// Compile-time interface check.
var _ crypto.WebhookDeliveryRepository = (*WebhookDeliveryRepo)(nil)
