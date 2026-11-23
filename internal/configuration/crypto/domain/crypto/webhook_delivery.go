// Package crypto provides webhook delivery persistence for merchant event audit trail.
package crypto

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// WebhookDelivery records a single webhook delivery attempt.
// Each retry creates a new row and a new DeliveryID. EventID stays stable for
// merchant-side idempotency.
type WebhookDelivery struct {
	ID             id.ID            `db:"id"               json:"id"`
	InvoiceID      *id.ID           `db:"invoice_id"       json:"invoiceId"` // nil for test webhooks
	MerchantID     id.ID            `db:"merchant_id"      json:"merchantId"`
	EventType      WebhookEventType `db:"event_type"       json:"eventType"`
	WebhookURL     string           `db:"webhook_url"      json:"webhookUrl"`
	EventID        string           `db:"event_id"         json:"eventId"`
	DeliveryID     string           `db:"delivery_id"      json:"deliveryId"`     // unique attempt ID
	StatusCode     *int             `db:"status_code"      json:"statusCode"`     // nil if connection error
	ResponseTimeMs *int             `db:"response_time_ms" json:"responseTimeMs"` // nil if connection error
	Attempt        int              `db:"attempt"          json:"attempt"`
	ErrorMessage   *string          `db:"error_message"    json:"errorMessage"`
	ErrorClass     string           `db:"error_class"      json:"errorClass,omitempty"`
	ResponseBody   string           `db:"response_body"    json:"responseBody,omitempty"`
	NextRetryAt    *time.Time       `db:"next_retry_at"    json:"nextRetryAt,omitempty"`
	RetryAfter     *time.Time       `db:"-"                json:"-"`
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

	// SetNextRetry records the scheduler decision on the corresponding attempt
	// so the merchant debugger and operations tooling see the same state.
	SetNextRetry(ctx context.Context, deliveryRecordID id.ID, next *time.Time) error

	// ListByInvoice returns delivery records for a specific invoice, ordered by created_at DESC.
	ListByInvoice(ctx context.Context, invoiceID id.ID, limit, offset int) ([]WebhookDelivery, int, error)

	// ListByMerchant returns delivery records for a merchant, ordered by created_at DESC.
	ListByMerchant(ctx context.Context, merchantID id.ID, limit, offset int) ([]WebhookDelivery, int, error)
}
