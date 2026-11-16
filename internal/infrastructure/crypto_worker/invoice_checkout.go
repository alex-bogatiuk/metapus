package crypto_worker

import (
	"context"
	"encoding/hex"
	"fmt"

	"metapus/internal/application/checkout"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto/publicref"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/storage/postgres"
)

// InvoiceCheckoutAggregateType separates new worker invoice registrations from
// historical automation events. Existing invoices are never scanned or backfilled.
const InvoiceCheckoutAggregateType = "invoice_checkout"

const _invoiceCheckoutCreated = "invoice_checkout.created"

type invoiceCheckoutPayload struct {
	RefHash    string `json:"refHash"`
	MerchantID id.ID  `json:"merchantId"`
}

// publishInvoiceCheckout writes the registration intent in the invoice's own
// transaction. Meta DB availability cannot interrupt payment processing.
func publishInvoiceCheckout(ctx context.Context, publisher domain.OutboxPublisher, invoice *crypto_invoice.CryptoInvoice) error {
	refHash, err := publicref.Hash(invoice.PublicRef)
	if err != nil {
		return fmt.Errorf("hash invoice checkout reference: %w", err)
	}
	if err := publisher.Publish(ctx, domain.DomainEvent{
		AggregateType: InvoiceCheckoutAggregateType,
		AggregateID:   invoice.ID,
		EventType:     _invoiceCheckoutCreated,
		Payload:       invoiceCheckoutPayload{RefHash: refHash, MerchantID: invoice.MerchantID},
	}); err != nil {
		return fmt.Errorf("queue invoice checkout reference: %w", err)
	}
	return nil
}

type invoiceCheckoutReferenceWriter interface {
	EnsureActive(context.Context, checkout.Reference) error
}

// InvoiceCheckoutHandler publishes committed worker invoice references to the
// meta registry. EnsureActive makes retries safe, including after a worker crash.
type InvoiceCheckoutHandler struct {
	refs invoiceCheckoutReferenceWriter
}

func NewInvoiceCheckoutHandler(refs invoiceCheckoutReferenceWriter) *InvoiceCheckoutHandler {
	return &InvoiceCheckoutHandler{refs: refs}
}

func (h *InvoiceCheckoutHandler) Handle(ctx context.Context, msg *postgres.OutboxMessage) error {
	if msg.AggregateType != InvoiceCheckoutAggregateType || msg.EventType != _invoiceCheckoutCreated {
		return fmt.Errorf("unsupported invoice checkout event")
	}
	var payload invoiceCheckoutPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal invoice checkout reference: %w", err)
	}
	refHash, err := hex.DecodeString(payload.RefHash)
	if err != nil || len(refHash) != 32 || id.IsNil(msg.AggregateID) || id.IsNil(payload.MerchantID) {
		return fmt.Errorf("invalid invoice checkout reference")
	}
	tenantID := tenant.GetTenantID(ctx)
	if tenantID == "" {
		return fmt.Errorf("invoice checkout registration requires tenant context")
	}
	if h.refs == nil {
		return fmt.Errorf("invoice checkout registry is not configured")
	}
	if err := h.refs.EnsureActive(ctx, checkout.Reference{
		RefHash:      payload.RefHash,
		Prefix:       publicref.InvoicePrefix,
		TenantID:     tenantID,
		MerchantID:   payload.MerchantID,
		ResourceType: checkout.ResourceTypeInvoice,
		ResourceID:   msg.AggregateID,
		Status:       checkout.StatusActive,
	}); err != nil {
		return fmt.Errorf("register invoice checkout reference: %w", err)
	}
	return nil
}

var _ postgres.OutboxHandler = (*InvoiceCheckoutHandler)(nil)
