package crypto

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/pkg/logger"
)

// TransitionMetadata holds structured context for FSM transitions.
// Replaces untyped map[string]interface{} for type safety (§2.4).
type TransitionMetadata struct {
	Confirmations int   `json:"confirmations,omitempty"`
	RequiredConfs int   `json:"requiredConfs,omitempty"`
	BlockNumber   int64 `json:"blockNumber,omitempty"`
	TxHash        string `json:"txHash,omitempty"`
}

// _allowedTransitions defines the payment state machine transition matrix.
// Key = current status, Value = list of allowed next statuses.
var _allowedTransitions = map[crypto_payment.PaymentStatus][]crypto_payment.PaymentStatus{
	crypto_payment.PaymentStatusDetected:   {crypto_payment.PaymentStatusConfirming},
	crypto_payment.PaymentStatusConfirming: {crypto_payment.PaymentStatusConfirmed, crypto_payment.PaymentStatusReorged},
	crypto_payment.PaymentStatusConfirmed:  {crypto_payment.PaymentStatusSettled},
	crypto_payment.PaymentStatusReorged:    {crypto_payment.PaymentStatusDetected},
}

// PaymentEvent represents an FSM event recorded in reg_crypto_payment_events.
type PaymentEvent struct {
	ID         id.ID                        `db:"id" json:"id"`
	PaymentID  id.ID                        `db:"payment_id" json:"paymentId"`
	FromStatus crypto_payment.PaymentStatus `db:"from_status" json:"fromStatus"`
	ToStatus   crypto_payment.PaymentStatus `db:"to_status" json:"toStatus"`
	EventType  string              `db:"event_type" json:"eventType"`
	Metadata   TransitionMetadata  `db:"metadata" json:"metadata"`
	CreatedAt  time.Time           `db:"created_at" json:"createdAt"`
}

// PaymentEventRepository persists FSM events for audit trail.
type PaymentEventRepository interface {
	Create(ctx context.Context, event *PaymentEvent) error
	GetByPaymentID(ctx context.Context, paymentID id.ID) ([]PaymentEvent, error)
}

// PaymentFSM manages payment state transitions with validation and audit logging.
type PaymentFSM struct {
	paymentRepo crypto_payment.Repository
	eventRepo   PaymentEventRepository
}

// NewPaymentFSM creates a new payment FSM.
func NewPaymentFSM(paymentRepo crypto_payment.Repository, eventRepo PaymentEventRepository) *PaymentFSM {
	return &PaymentFSM{
		paymentRepo: paymentRepo,
		eventRepo:   eventRepo,
	}
}

// Transition performs a validated state transition on a payment.
// Records the transition event and updates the payment.
// Returns error if transition is not allowed or audit trail cannot be recorded.
//
// Audit trail is mandatory for financial traceability — if the FSM event
// cannot be persisted, the entire transaction is rolled back.
func (fsm *PaymentFSM) Transition(
	ctx context.Context,
	payment *crypto_payment.CryptoPayment,
	newStatus crypto_payment.PaymentStatus,
	eventType string,
	metadata TransitionMetadata,
) error {
	// 1. Validate transition
	if !fsm.isAllowed(payment.Status, newStatus) {
		return apperror.NewValidation(
			fmt.Sprintf("transition %s → %s is not allowed", payment.Status, newStatus),
		).WithDetail("currentStatus", string(payment.Status)).
			WithDetail("newStatus", string(newStatus))
	}

	oldStatus := payment.Status

	// 2. Apply transition
	payment.Status = newStatus
	if newStatus == crypto_payment.PaymentStatusConfirmed {
		now := time.Now().UTC()
		payment.ConfirmedAt = &now
	}

	// 3. Persist payment
	if err := fsm.paymentRepo.Update(ctx, payment); err != nil {
		return fmt.Errorf("update payment status: %w", err)
	}

	// 4. Record event (audit trail — mandatory for financial traceability)
	event := &PaymentEvent{
		ID:         id.New(),
		PaymentID:  payment.ID,
		FromStatus: oldStatus,
		ToStatus:   newStatus,
		EventType:  eventType,
		Metadata:   metadata,
		CreatedAt:  time.Now().UTC(),
	}

	if err := fsm.eventRepo.Create(ctx, event); err != nil {
		return fmt.Errorf("record payment FSM event: %w", err)
	}

	logger.Info(ctx, "payment status transition",
		"payment_id", payment.ID,
		"from_status", oldStatus,
		"to_status", newStatus,
		"event_type", eventType,
	)

	return nil
}

// isAllowed checks if a transition is valid per the transition matrix.
func (fsm *PaymentFSM) isAllowed(from, to crypto_payment.PaymentStatus) bool {
	allowed, ok := _allowedTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// GetHistory returns the FSM event history for a payment.
func (fsm *PaymentFSM) GetHistory(ctx context.Context, paymentID id.ID) ([]PaymentEvent, error) {
	return fsm.eventRepo.GetByPaymentID(ctx, paymentID)
}
