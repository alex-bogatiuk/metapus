package crypto

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

const WebhookSecretRotationOverlap = 24 * time.Hour

// MerchantWebhookEndpoint is a merchant-managed delivery destination. Secret
// material is deliberately absent: PostgreSQL stores opaque Vault references.
type MerchantWebhookEndpoint struct {
	ID                       id.ID              `db:"id" json:"id"`
	MerchantID               id.ID              `db:"merchant_id" json:"-"`
	Name                     string             `db:"name" json:"name"`
	URL                      string             `db:"url" json:"url"`
	EnabledEvents            []WebhookEventType `db:"enabled_events" json:"enabledEvents"`
	IsDefault                bool               `db:"is_default" json:"isDefault"`
	IsActive                 bool               `db:"is_active" json:"isActive"`
	SecretRef                string             `db:"secret_ref" json:"-"`
	SecretVersion            int                `db:"secret_version" json:"secretVersion"`
	PreviousSecretRef        string             `db:"previous_secret_ref" json:"-"`
	PreviousSecretVersion    *int               `db:"previous_secret_version" json:"-"`
	PreviousSecretValidUntil *time.Time         `db:"previous_secret_valid_until" json:"previousSecretValidUntil,omitempty"`
	Version                  int                `db:"version" json:"version"`
	CreatedAt                time.Time          `db:"created_at" json:"createdAt"`
	UpdatedAt                time.Time          `db:"updated_at" json:"updatedAt"`
}

type MerchantEventRecord struct {
	ID              id.ID            `db:"id" json:"-"`
	EventID         string           `db:"event_id" json:"id"`
	MerchantID      id.ID            `db:"merchant_id" json:"-"`
	InvoiceID       *id.ID           `db:"invoice_id" json:"invoiceId,omitempty"`
	EventType       WebhookEventType `db:"event_type" json:"type"`
	APIVersion      string           `db:"api_version" json:"apiVersion"`
	ResourceVersion int              `db:"resource_version" json:"resourceVersion"`
	Payload         []byte           `db:"payload" json:"payload"`
	CreatedAt       time.Time        `db:"created_at" json:"createdAt"`
}

type WebhookDeliveryJob struct {
	ClaimToken               id.ID  `db:"claim_token"`
	EventPayload             []byte `db:"event_payload"`
	ID                       id.ID
	EventID                  string
	EndpointID               *id.ID
	MerchantID               id.ID
	SnapshotURL              string
	SecretRef                string
	SecretVersion            int
	PreviousSecretRef        string
	PreviousSecretValidUntil *time.Time
	AttemptCount             int
	DeadlineAt               time.Time
	Manual                   bool
	ScheduleStartedAt        time.Time `db:"schedule_started_at"`
}

type WebhookSecretRetirement struct {
	SecretRef    string `db:"secret_ref"`
	AttemptCount int    `db:"attempt_count"`
}

// WebhookSecretRetirementRepository durably delays Vault deletion until no
// automatic or manual delivery snapshot can still reference the secret.
type WebhookSecretRetirementRepository interface {
	ClaimDueSecretRetirements(ctx context.Context, now time.Time, limit int) ([]WebhookSecretRetirement, error)
	CompleteSecretRetirement(ctx context.Context, secretRef string) error
	RetrySecretRetirement(ctx context.Context, secretRef string, next time.Time, lastError string) error
}

// WebhookSecretStore owns the only persisted copy of signing secrets.
type WebhookSecretStore interface {
	Put(ctx context.Context, secretRef, secret string) error
	Get(ctx context.Context, secretRef string) (string, error)
	Delete(ctx context.Context, secretRef string) error
}

type MerchantWebhookRepository interface {
	CreateEndpoint(ctx context.Context, endpoint *MerchantWebhookEndpoint) error
	GetEndpoint(ctx context.Context, merchantID, endpointID id.ID) (*MerchantWebhookEndpoint, error)
	ListEndpoints(ctx context.Context, merchantID id.ID) ([]MerchantWebhookEndpoint, error)
	UpdateEndpoint(ctx context.Context, endpoint *MerchantWebhookEndpoint) error
	DeleteEndpoint(ctx context.Context, merchantID, endpointID id.ID) error
	HasDefaultEndpoint(ctx context.Context, merchantID id.ID) (bool, error)
	RotateEndpointSecret(ctx context.Context, merchantID, endpointID id.ID, newRef string, now time.Time) (*MerchantWebhookEndpoint, error)

	ListEvents(ctx context.Context, merchantID id.ID, limit, offset int) ([]MerchantEventRecord, int, error)
	GetEvent(ctx context.Context, merchantID id.ID, eventID string) (*MerchantEventRecord, error)
	ListEventDeliveries(ctx context.Context, merchantID id.ID, eventID string) ([]WebhookDelivery, error)
	PrepareAutomaticJobs(ctx context.Context, event LifecycleWebhookEvent) error
	ListDueJobs(ctx context.Context, eventID string, now time.Time) ([]WebhookDeliveryJob, bool, error)
	ClaimDueJobs(ctx context.Context, now time.Time, limit int) ([]WebhookDeliveryJob, error)
	RenewJob(ctx context.Context, jobID, claimToken id.ID) error
	MarkJobSucceeded(ctx context.Context, jobID, claimToken id.ID) error
	MarkJobRetry(ctx context.Context, jobID, claimToken id.ID, attempt int, next time.Time, terminal bool, lastError string) error
	CreateManualJob(ctx context.Context, merchantID id.ID, eventID string, endpointID id.ID, now time.Time) error
}

func IsBaseInvoiceEvent(event WebhookEventType) bool {
	switch event {
	case WebhookInvoiceCreated, WebhookInvoicePaid, WebhookInvoiceConfirmed,
		WebhookInvoiceCompleted, WebhookInvoiceOverpaid, WebhookInvoiceUnderpaid,
		WebhookInvoiceExpired, WebhookInvoiceCancelled, WebhookInvoiceReversed,
		WebhookInvoiceLatePayment:
		return true
	default:
		return false
	}
}

func AllWebhookEventTypes() []WebhookEventType {
	return []WebhookEventType{
		WebhookWalletRiskChanged,
		WebhookDepositAddressRotated,
		WebhookInvoiceCreated, WebhookInvoicePaid, WebhookInvoiceConfirmed,
		WebhookInvoiceCompleted, WebhookInvoiceOverpaid, WebhookInvoiceUnderpaid,
		WebhookInvoiceExpired, WebhookInvoiceCancelled, WebhookInvoiceReversed,
		WebhookInvoiceLatePayment,
		WebhookPaymentDetected, WebhookPaymentConfirming, WebhookPaymentConfirmed,
		WebhookPaymentReorged, WebhookPaymentReplaced,
		WebhookWithdrawalConfirmed, WebhookWithdrawalFailed, WebhookWithdrawalPartiallyCompleted,
	}
}

func IsKnownWebhookEventType(value WebhookEventType) bool {
	for _, candidate := range AllWebhookEventTypes() {
		if candidate == value {
			return true
		}
	}
	return false
}
