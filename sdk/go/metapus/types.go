package metapus

import "time"

type EventType string

const (
	EventInvoiceCreated               EventType = "invoice.created"
	EventInvoicePaid                  EventType = "invoice.paid"
	EventInvoiceConfirmed             EventType = "invoice.confirmed"
	EventInvoiceCompleted             EventType = "invoice.completed"
	EventInvoiceOverpaid              EventType = "invoice.overpaid"
	EventInvoiceUnderpaid             EventType = "invoice.underpaid"
	EventInvoiceExpired               EventType = "invoice.expired"
	EventInvoiceCancelled             EventType = "invoice.cancelled"
	EventInvoiceReversed              EventType = "invoice.reversed"
	EventInvoiceLatePayment           EventType = "invoice.late_payment"
	EventPaymentDetected              EventType = "payment.detected"
	EventPaymentConfirming            EventType = "payment.confirming"
	EventPaymentConfirmed             EventType = "payment.confirmed"
	EventPaymentReorged               EventType = "payment.reorged"
	EventPaymentReplaced              EventType = "payment.replaced"
	EventWithdrawalConfirmed          EventType = "withdrawal.confirmed"
	EventWithdrawalFailed             EventType = "withdrawal.failed"
	EventWithdrawalPartiallyCompleted EventType = "withdrawal.partially_completed"
	EventWalletRiskChanged            EventType = "wallet.risk_changed"
	EventDepositAddressRotated        EventType = "deposit_address.rotated"
)

type CreateWebhookEndpointRequest struct {
	Name          string      `json:"name"`
	URL           string      `json:"url"`
	EnabledEvents []EventType `json:"enabledEvents"`
	IsDefault     bool        `json:"isDefault"`
}

type UpdateWebhookEndpointRequest struct {
	Version       int          `json:"version"`
	Name          *string      `json:"name,omitempty"`
	URL           *string      `json:"url,omitempty"`
	EnabledEvents *[]EventType `json:"enabledEvents,omitempty"`
	IsDefault     *bool        `json:"isDefault,omitempty"`
	IsActive      *bool        `json:"isActive,omitempty"`
}

type WebhookEndpoint struct {
	ID                       string      `json:"id"`
	Object                   string      `json:"object"`
	Name                     string      `json:"name"`
	URL                      string      `json:"url"`
	EnabledEvents            []EventType `json:"enabledEvents"`
	IsDefault                bool        `json:"isDefault"`
	IsActive                 bool        `json:"isActive"`
	SecretVersion            int         `json:"secretVersion"`
	Version                  int         `json:"version"`
	Secret                   string      `json:"secret,omitempty"`
	PreviousSecretValidUntil *time.Time  `json:"previousSecretValidUntil,omitempty"`
	CreatedAt                time.Time   `json:"createdAt"`
	UpdatedAt                time.Time   `json:"updatedAt"`
}

type ListResponse[T any] struct {
	Object string `json:"object"`
	Items  []T    `json:"items"`
	Total  int    `json:"total"`
}

type Payment struct {
	ID                    string     `json:"id"`
	TxHash                string     `json:"txHash"`
	LogIndex              int64      `json:"logIndex"`
	Amount                string     `json:"amount"`
	Status                string     `json:"status"`
	Canonicality          string     `json:"canonicality"`
	FinalityStatus        string     `json:"finalityStatus"`
	Confirmations         int        `json:"confirmations"`
	RequiredConfirmations int        `json:"requiredConfirmations"`
	SourceAddress         string     `json:"sourceAddress,omitempty"`
	SourceNonce           *int64     `json:"sourceNonce,omitempty"`
	ReplacementTxHash     string     `json:"replacementTxHash,omitempty"`
	DetectedAt            time.Time  `json:"detectedAt"`
	ConfirmedAt           *time.Time `json:"confirmedAt,omitempty"`
}

// BindInvoiceReferenceRequest assigns merchant business references to a persistent top-up.
// At least one reference is required; omitted references keep their stored values.
// Metadata nil preserves the dictionary; a pointer to an empty map clears it.
type BindInvoiceReferenceRequest struct {
	OrderID    string             `json:"orderId,omitempty"`
	ExternalID string             `json:"externalId,omitempty"`
	Metadata   *map[string]string `json:"metadata,omitempty"`
}

type Invoice struct {
	ID                    string            `json:"id"`
	Status                string            `json:"status"`
	ResourceVersion       int               `json:"resourceVersion"`
	ExpectedAmount        string            `json:"expectedAmount"`
	ReceivedAmount        string            `json:"receivedAmount"`
	ConfirmedAmount       string            `json:"confirmedAmount"`
	CreditedAmount        string            `json:"creditedAmount"`
	OverpaidAmount        string            `json:"overpaidAmount"`
	Currency              string            `json:"currency"`
	Network               string            `json:"network"`
	RequiredConfirmations int               `json:"requiredConfirmations"`
	FinalityMode          string            `json:"finalityMode"`
	OrderID               string            `json:"orderId,omitempty"`
	ExternalID            string            `json:"externalId,omitempty"`
	Metadata              map[string]string `json:"metadata"`
	PaidAt                *time.Time        `json:"paidAt,omitempty"`
	ConfirmedAt           *time.Time        `json:"confirmedAt,omitempty"`
	CompletedAt           *time.Time        `json:"completedAt,omitempty"`
	ReversedAt            *time.Time        `json:"reversedAt,omitempty"`
	AsOf                  time.Time         `json:"asOf"`
	ChainObservedAt       *time.Time        `json:"chainObservedAt,omitempty"`
	Payments              []Payment         `json:"payments"`
}

type WebhookTestResult struct {
	EventID    string `json:"eventId"`
	DeliveryID string `json:"deliveryId,omitempty"`
	Success    bool   `json:"success"`
	StatusCode *int   `json:"statusCode,omitempty"`
	DurationMs *int   `json:"durationMs,omitempty"`
	Error      string `json:"error,omitempty"`
}
