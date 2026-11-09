// Package crypto declares merchant webhook event and delivery contracts.
package crypto

import (
	"context"
	"metapus/internal/core/id"
	"time"
)

// WebhookEventType defines the types of events that trigger merchant webhooks.
type WebhookEventType string

const WebhookWalletRiskChanged WebhookEventType = "wallet.risk_changed"
const WebhookDepositAddressRotated WebhookEventType = "deposit_address.rotated"

const (
	WebhookInvoiceCreated               WebhookEventType = "invoice.created"
	WebhookInvoicePaid                  WebhookEventType = "invoice.paid"
	WebhookInvoiceConfirmed             WebhookEventType = "invoice.confirmed"
	WebhookInvoiceCompleted             WebhookEventType = "invoice.completed"
	WebhookInvoiceOverpaid              WebhookEventType = "invoice.overpaid"
	WebhookInvoiceUnderpaid             WebhookEventType = "invoice.underpaid"
	WebhookInvoiceExpired               WebhookEventType = "invoice.expired"
	WebhookInvoiceCancelled             WebhookEventType = "invoice.cancelled"
	WebhookInvoiceReversed              WebhookEventType = "invoice.reversed"
	WebhookInvoiceLatePayment           WebhookEventType = "invoice.late_payment"
	WebhookPaymentDetected              WebhookEventType = "payment.detected"
	WebhookPaymentConfirming            WebhookEventType = "payment.confirming"
	WebhookPaymentConfirmed             WebhookEventType = "payment.confirmed"
	WebhookPaymentReorged               WebhookEventType = "payment.reorged"
	WebhookPaymentReplaced              WebhookEventType = "payment.replaced"
	WebhookWithdrawalConfirmed          WebhookEventType = "withdrawal.confirmed"
	WebhookWithdrawalFailed             WebhookEventType = "withdrawal.failed"
	WebhookWithdrawalPartiallyCompleted WebhookEventType = "withdrawal.partially_completed"
)

// WebhookPayload is the payload sent to merchant's webhook URL.
type WebhookPayload struct {
	ID         string           `json:"id"`
	Object     string           `json:"object"`
	APIVersion string           `json:"apiVersion"`
	Type       WebhookEventType `json:"type"`
	CreatedAt  time.Time        `json:"createdAt"`
	Data       WebhookEventData `json:"data"`
}

type WebhookEventData struct {
	Object map[string]any `json:"object"`
}

// WebhookDispatchRequest contains one immutable event delivery attempt. EventID
// identifies the business event and remains stable across retries; DeliveryID
// is generated separately for every HTTP attempt.
type WebhookDispatchRequest struct {
	InvoiceID      *id.ID
	MerchantID     id.ID
	WebhookURL     string
	ActiveSecret   string
	PreviousSecret string
	EventID        string
	APIVersion     string
	Event          WebhookEventType
	OccurredAt     time.Time
	Data           map[string]any
	RawBody        []byte
	Attempt        int
	Traceparent    string
	CorrelationID  string
	CausationID    string
}

// WebhookDispatcher is the outbound delivery port. HTTP and DNS belong to adapters.
type WebhookDispatcher interface {
	Dispatch(context.Context, WebhookDeliveryRepository, WebhookDispatchRequest) (*WebhookDelivery, error)
}
