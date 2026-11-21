package crypto_worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/checkout"
	"metapus/internal/configuration/crypto/domain/crypto/publicref"
	"metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"
)

type checkoutEventCapture struct {
	event domain.DomainEvent
	err   error
}

func (p *checkoutEventCapture) Publish(_ context.Context, event domain.DomainEvent) error {
	p.event = event
	return p.err
}

type checkoutReferenceCapture struct {
	refs []checkout.Reference
	err  error
}

func (r *checkoutReferenceCapture) EnsureActive(_ context.Context, ref checkout.Reference) error {
	if r.err != nil {
		return r.err
	}
	r.refs = append(r.refs, ref)
	return nil
}

func checkoutMessage(t *testing.T) (*crypto_invoice.CryptoInvoice, *postgres.OutboxMessage) {
	t.Helper()
	invoice := crypto_invoice.NewCryptoInvoice(id.New(), id.New(), types.NewRawAmountFromInt64(1000000))
	publisher := &checkoutEventCapture{}
	require.NoError(t, publishInvoiceCheckout(context.Background(), publisher, invoice))
	payload, err := json.Marshal(publisher.event.Payload)
	require.NoError(t, err)
	return invoice, &postgres.OutboxMessage{
		AggregateType: publisher.event.AggregateType, AggregateID: publisher.event.AggregateID,
		EventType: publisher.event.EventType, Payload: payload,
	}
}

func TestInvoiceCheckoutQueuesOnlyHashedReference(t *testing.T) {
	invoice, msg := checkoutMessage(t)
	require.Equal(t, invoice.ID, msg.AggregateID)
	require.NotContains(t, string(msg.Payload), invoice.PublicRef)
	var payload invoiceCheckoutPayload
	require.NoError(t, json.Unmarshal(msg.Payload, &payload))
	hash, err := publicref.Hash(invoice.PublicRef)
	require.NoError(t, err)
	require.Equal(t, hash, payload.RefHash)
	require.Equal(t, invoice.MerchantID, payload.MerchantID)

	publisher := &checkoutEventCapture{err: errors.New("outbox unavailable")}
	require.ErrorIs(t, publishInvoiceCheckout(context.Background(), publisher, invoice), publisher.err)
}

func TestInvoiceCheckoutRetriesRegistrationWithoutExpiry(t *testing.T) {
	invoice, msg := checkoutMessage(t)
	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: id.New().String()})
	refs := &checkoutReferenceCapture{err: errors.New("meta unavailable")}
	require.ErrorIs(t, NewInvoiceCheckoutHandler(refs).Handle(ctx, msg), refs.err)
	require.Empty(t, refs.refs)

	refs.err = nil
	// A fresh handler models redelivery after a worker restart.
	require.NoError(t, NewInvoiceCheckoutHandler(refs).Handle(ctx, msg))
	require.Len(t, refs.refs, 1)
	ref := refs.refs[0]
	require.Equal(t, tenant.GetTenantID(ctx), ref.TenantID)
	require.Equal(t, invoice.MerchantID, ref.MerchantID)
	require.Equal(t, invoice.ID, ref.ResourceID)
	require.Equal(t, checkout.ResourceTypeInvoice, ref.ResourceType)
	require.Equal(t, checkout.StatusActive, ref.Status)
	require.Equal(t, publicref.InvoicePrefix, ref.Prefix)
	require.Nil(t, ref.ExpiresAt, "completed invoices must remain readable after the payment deadline")
}

func TestInvoiceCheckoutRejectsHistoricalAndInvalidEvents(t *testing.T) {
	_, msg := checkoutMessage(t)
	ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: id.New().String()})
	tests := []struct {
		name   string
		change func(*postgres.OutboxMessage)
		ctx    context.Context
	}{
		{name: "historical automation event", ctx: ctx, change: func(m *postgres.OutboxMessage) { m.AggregateType = "crypto_invoice" }},
		{name: "status change", ctx: ctx, change: func(m *postgres.OutboxMessage) { m.EventType = "invoice_checkout.updated" }},
		{name: "invalid payload", ctx: ctx, change: func(m *postgres.OutboxMessage) { m.Payload = []byte("{") }},
		{name: "missing merchant and hash", ctx: ctx, change: func(m *postgres.OutboxMessage) { m.Payload = []byte("{}") }},
		{name: "missing invoice", ctx: ctx, change: func(m *postgres.OutboxMessage) { m.AggregateID = id.Nil() }},
		{name: "missing tenant", ctx: context.Background(), change: func(*postgres.OutboxMessage) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copy := *msg
			tt.change(&copy)
			refs := &checkoutReferenceCapture{}
			require.Error(t, NewInvoiceCheckoutHandler(refs).Handle(tt.ctx, &copy))
			require.Empty(t, refs.refs)
		})
	}
}
