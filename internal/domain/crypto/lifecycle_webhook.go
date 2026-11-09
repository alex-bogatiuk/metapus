package crypto

import (
	"context"
	"maps"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_invoice"
)

const MerchantWebhookAggregateType = "merchant_webhook"
const MerchantWebhookAPIVersion = "2026-08-26"

// LifecycleWebhookEvent is the immutable event stored in the transactional
// outbox. Delivery retries reuse both OccurredAt and the outbox message ID.
type LifecycleWebhookEvent struct {
	ID              string           `json:"id"`
	APIVersion      string           `json:"apiVersion"`
	Event           WebhookEventType `json:"event"`
	AggregateID     id.ID            `json:"aggregateId"`
	MerchantID      id.ID            `json:"merchantId"`
	InvoiceID       *id.ID           `json:"invoiceId,omitempty"`
	ResourceVersion int              `json:"resourceVersion"`
	CausationKey    string           `json:"causationKey"`
	OccurredAt      time.Time        `json:"occurredAt"`
	Data            map[string]any   `json:"data"`
	// DeliveryPayload is the exact immutable public JSON body generated once by
	// the application publisher and persisted inside the outbox envelope.
	DeliveryPayload []byte `json:"deliveryPayload,omitempty"`
}

// LifecycleWebhookPublisher persists a merchant webhook event atomically with
// the business state change that caused it.
type LifecycleWebhookPublisher interface {
	PublishLifecycleWebhook(ctx context.Context, event LifecycleWebhookEvent) error
}

// InvoiceWebhookData builds the stable public invoice snapshot shared by all
// invoice lifecycle events.
func InvoiceWebhookData(invoice *crypto_invoice.CryptoInvoice, extra map[string]any) map[string]any {
	data := map[string]any{
		"id":                    invoice.ID.String(),
		"invoiceId":             invoice.ID.String(),
		"status":                invoice.Status,
		"acceptsPayments":       invoice.AcceptsPayments(),
		"resourceVersion":       invoice.Version,
		"orderId":               invoice.OrderID,
		"metadata":              invoice.Metadata,
		"expectedAmount":        invoice.ExpectedAmount.String(),
		"receivedAmount":        invoice.ReceivedAmount.String(),
		"confirmedAmount":       invoice.ConfirmedAmount.String(),
		"creditedAmount":        invoice.CreditedAmount.String(),
		"overpaidAmount":        invoice.OverpaidAmount.String(),
		"tokenId":               invoice.TokenID.String(),
		"requiredConfirmations": invoice.RequiredConfirmations,
		"finalityMode":          invoice.FinalityMode,
	}
	maps.Copy(data, extra)
	return data
}
