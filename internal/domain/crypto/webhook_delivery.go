// Package crypto provides webhook delivery persistence for merchant event audit trail.
package crypto

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// WebhookDelivery records a single webhook delivery attempt.
// Each retry creates a new row — immutable audit trail (Stripe Events pattern).
type WebhookDelivery struct {
	ID             id.ID            `db:"id"               json:"id"`
	InvoiceID      *id.ID           `db:"invoice_id"       json:"invoiceId"`      // nil for test webhooks
	MerchantID     id.ID            `db:"merchant_id"      json:"merchantId"`
	EventType      WebhookEventType `db:"event_type"       json:"eventType"`
	WebhookURL     string           `db:"webhook_url"      json:"webhookUrl"`
	DeliveryID     string           `db:"delivery_id"      json:"deliveryId"`     // X-Metapus-Delivery-ID
	StatusCode     *int             `db:"status_code"      json:"statusCode"`     // nil if connection error
	ResponseTimeMs *int             `db:"response_time_ms" json:"responseTimeMs"` // nil if connection error
	Attempt        int              `db:"attempt"          json:"attempt"`
	ErrorMessage   *string          `db:"error_message"    json:"errorMessage"`
	RequestBody    []byte           `db:"request_body"     json:"-"` // JSON, excluded from default serialization
	CreatedAt      time.Time        `db:"created_at"       json:"createdAt"`
}

// IsSuccess returns true if the delivery received a 2xx response.
func (d *WebhookDelivery) IsSuccess() bool {
	return d.StatusCode != nil && *d.StatusCode >= 200 && *d.StatusCode < 300
}

// WebhookDeliveryRepository persists webhook delivery records.
type WebhookDeliveryRepository interface {
	// Create inserts a new delivery record.
	Create(ctx context.Context, delivery *WebhookDelivery) error

	// ListByInvoice returns delivery records for a specific invoice, ordered by created_at DESC.
	ListByInvoice(ctx context.Context, invoiceID id.ID, limit, offset int) ([]WebhookDelivery, int, error)

	// ListByMerchant returns delivery records for a merchant, ordered by created_at DESC.
	ListByMerchant(ctx context.Context, merchantID id.ID, limit, offset int) ([]WebhookDelivery, int, error)
}
