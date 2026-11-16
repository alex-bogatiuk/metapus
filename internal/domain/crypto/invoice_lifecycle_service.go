package crypto

import (
	"context"
	"errors"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_payment"
)

var ErrInvoiceNotCancellable = errors.New("invoice is not cancellable")

// InvoiceLifecycleService is the single writer for aggregate invoice state.
// It derives the projection from canonical payment facts under an invoice lock
// and emits merchant events in the same transaction.
type InvoiceLifecycleService struct {
	invoices  InvoiceAccessor
	payments  crypto_payment.Repository
	publisher LifecycleWebhookPublisher
	txm       tx.Manager
	now       func() time.Time
}

func NewInvoiceLifecycleService(invoices InvoiceAccessor, payments crypto_payment.Repository, publisher LifecycleWebhookPublisher, txm tx.Manager, now func() time.Time) *InvoiceLifecycleService {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &InvoiceLifecycleService{invoices: invoices, payments: payments, publisher: publisher, txm: txm, now: now}
}

func (s *InvoiceLifecycleService) Reconcile(ctx context.Context, invoiceID id.ID) (bool, error) {
	var completed bool
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		completed, err = s.SyncInTransaction(txCtx, invoiceID)
		return err
	})
	return completed, err
}

func (s *InvoiceLifecycleService) SyncInTransaction(ctx context.Context, invoiceID id.ID) (bool, error) {
	invoice, err := s.invoices.GetByIDForUpdate(ctx, invoiceID)
	if err != nil {
		return false, fmt.Errorf("lock invoice %s: %w", invoiceID, err)
	}
	summary, err := s.payments.SummarizeByInvoice(ctx, invoiceID)
	if err != nil {
		return false, fmt.Errorf("summarize invoice %s payments: %w", invoiceID, err)
	}

	previousReceived := invoice.ReceivedAmount
	previousConfirmed := invoice.ConfirmedAmount
	previousCredited := invoice.CreditedAmount
	previousOverpaid := invoice.OverpaidAmount
	previousStatus := invoice.Status
	saved := false
	invoice.ReceivedAmount = summary.ReceivedAmount
	invoice.ConfirmedAmount = summary.ConfirmedAmount
	invoice.CreditedAmount = summary.CreditedAmount
	if summary.ReceivedAmount.Cmp(invoice.ExpectedAmount) > 0 {
		invoice.OverpaidAmount = summary.ReceivedAmount.Sub(invoice.ExpectedAmount)
	} else {
		invoice.OverpaidAmount = types.ZeroRawAmount()
	}

	if InvoiceIsLateTerminal(previousStatus) {
		return false, s.saveIfProjectionChanged(ctx, invoice, previousReceived, previousConfirmed, previousCredited, previousOverpaid, previousStatus)
	}
	if invoice.SinglePayment && invoice.SelectedPaymentID != nil &&
		summary.CreditedAmount.IsPositive() && summary.CreditedAmount.Cmp(invoice.ExpectedAmount) < 0 {
		invoice.Status = crypto_invoice.InvoiceStatusUnderpaid
		if previousStatus != crypto_invoice.InvoiceStatusUnderpaid {
			if err := s.invoices.Update(ctx, invoice); err != nil {
				return false, err
			}
			extra := map[string]any{"underpaidAmount": invoice.ExpectedAmount.Sub(invoice.ReceivedAmount).String()}
			if err := s.publishWithExtra(ctx, WebhookInvoiceUnderpaid, invoice, extra); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	// A finalized/completed invoice that loses finalized canonical coverage is
	// compensated and becomes reversed. It never silently moves backwards.
	if (previousStatus == crypto_invoice.InvoiceStatusConfirmed ||
		previousStatus == crypto_invoice.InvoiceStatusCompleted ||
		previousStatus == crypto_invoice.InvoiceStatusReversed) &&
		summary.ConfirmedAmount.Cmp(invoice.ExpectedAmount) < 0 {
		invoice.Status = crypto_invoice.InvoiceStatusReversed
		if previousStatus != crypto_invoice.InvoiceStatusReversed {
			now := s.now().UTC()
			invoice.ReversedAt = &now
			if err := s.invoices.Update(ctx, invoice); err != nil {
				return false, err
			}
			return false, s.publish(ctx, WebhookInvoiceReversed, invoice)
		}
		return false, s.saveIfProjectionChanged(ctx, invoice, previousReceived, previousConfirmed, previousCredited, previousOverpaid, previousStatus)
	}

	// Observed amount states precede network/accounting finality.
	if summary.ReceivedAmount.Cmp(invoice.ExpectedAmount) >= 0 {
		invoice.Status = crypto_invoice.InvoiceStatusPaid
		if previousReceived.Cmp(invoice.ExpectedAmount) < 0 || previousStatus == crypto_invoice.InvoiceStatusReversed ||
			previousStatus == crypto_invoice.InvoiceStatusCreated || previousStatus == crypto_invoice.InvoiceStatusPartiallyPaid {
			now := s.now().UTC()
			invoice.PaidAt = &now
			if err := s.invoices.Update(ctx, invoice); err != nil {
				return false, err
			}
			saved = true
			if err := s.publish(ctx, WebhookInvoicePaid, invoice); err != nil {
				return false, err
			}
		}
	} else if summary.ReceivedAmount.IsPositive() {
		invoice.Status = crypto_invoice.InvoiceStatusPartiallyPaid
	} else {
		invoice.Status = crypto_invoice.InvoiceStatusCreated
	}
	if previousOverpaid.IsZero() && invoice.OverpaidAmount.IsPositive() {
		if err := s.invoices.Update(ctx, invoice); err != nil {
			return false, err
		}
		saved = true
		if err := s.publish(ctx, WebhookInvoiceOverpaid, invoice); err != nil {
			return false, err
		}
	}

	if summary.ConfirmedAmount.Cmp(invoice.ExpectedAmount) >= 0 &&
		previousStatus == crypto_invoice.InvoiceStatusCompleted && summary.CreditedAmount.Cmp(invoice.ExpectedAmount) < 0 {
		return false, fmt.Errorf("completed invoice %s lost credited ledger coverage without a canonical finality loss", invoice.ID)
	}
	if summary.ConfirmedAmount.Cmp(invoice.ExpectedAmount) >= 0 {
		invoice.Status = crypto_invoice.InvoiceStatusConfirmed
		if previousStatus != crypto_invoice.InvoiceStatusConfirmed && previousStatus != crypto_invoice.InvoiceStatusCompleted {
			now := s.now().UTC()
			invoice.ConfirmedAt = &now
			if err := s.invoices.Update(ctx, invoice); err != nil {
				return false, err
			}
			saved = true
			if err := s.publish(ctx, WebhookInvoiceConfirmed, invoice); err != nil {
				return false, err
			}
		}
	}

	if summary.CreditedAmount.Cmp(invoice.ExpectedAmount) >= 0 {
		invoice.Status = crypto_invoice.InvoiceStatusCompleted
		if previousStatus != crypto_invoice.InvoiceStatusCompleted {
			now := s.now().UTC()
			invoice.CompletedAt = &now
			if err := s.invoices.Update(ctx, invoice); err != nil {
				return false, err
			}
			if err := s.publish(ctx, WebhookInvoiceCompleted, invoice); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	if saved {
		return false, nil
	}
	return false, s.saveIfProjectionChanged(ctx, invoice, previousReceived, previousConfirmed, previousCredited, previousOverpaid, previousStatus)
}

// ExpireInTransaction applies the time-driven terminal transition using the
// same canonical payment projection and publisher as chain-driven changes.
// It locks the invoice first. If the returned releaseWallet value is true, the
// caller may then lock and release the assigned wallet in the same transaction.
// This preserves the global invoice -> wallet ordering used by payment finality.
func (s *InvoiceLifecycleService) ExpireInTransaction(ctx context.Context, invoiceID id.ID) (bool, error) {
	invoice, err := s.invoices.GetByIDForUpdate(ctx, invoiceID)
	if err != nil {
		return false, fmt.Errorf("lock invoice %s for expiration: %w", invoiceID, err)
	}
	if invoice.Status != crypto_invoice.InvoiceStatusCreated && invoice.Status != crypto_invoice.InvoiceStatusPartiallyPaid {
		return false, nil
	}
	if invoice.ExpiresAt.After(s.now().UTC()) {
		return false, nil
	}
	// Once a transfer is selected, network finality decides the result even if
	// the original payment window expires while confirmations are pending.
	if invoice.SinglePayment && invoice.SelectedPaymentID != nil {
		return false, nil
	}
	summary, err := s.payments.SummarizeByInvoice(ctx, invoiceID)
	if err != nil {
		return false, fmt.Errorf("summarize expiring invoice %s payments: %w", invoiceID, err)
	}
	// A concurrent full payment wins over expiry. Re-run the ordinary policy so
	// paid/confirmed/completed events are emitted instead of a false expiry.
	if summary.ReceivedAmount.Cmp(invoice.ExpectedAmount) >= 0 {
		return s.SyncInTransaction(ctx, invoiceID)
	}
	invoice.ReceivedAmount = summary.ReceivedAmount
	invoice.ConfirmedAmount = summary.ConfirmedAmount
	invoice.CreditedAmount = summary.CreditedAmount
	invoice.OverpaidAmount = types.ZeroRawAmount()
	eventType := WebhookInvoiceExpired
	if summary.ReceivedAmount.IsPositive() {
		invoice.Status = crypto_invoice.InvoiceStatusUnderpaid
		eventType = WebhookInvoiceUnderpaid
	} else {
		invoice.Status = crypto_invoice.InvoiceStatusExpired
	}
	if err := s.invoices.Update(ctx, invoice); err != nil {
		return false, err
	}
	extra := map[string]any{}
	if invoice.Status == crypto_invoice.InvoiceStatusUnderpaid {
		extra["underpaidAmount"] = invoice.ExpectedAmount.Sub(invoice.ReceivedAmount).String()
	}
	if err := s.publishWithExtra(ctx, eventType, invoice, extra); err != nil {
		return false, err
	}
	return true, nil
}

// CancelInTransaction performs the merchant-driven terminal transition through
// the same aggregate writer and transactional event publisher as chain and
// expiration transitions. A payment that already reached network finality wins
// over cancellation.
func (s *InvoiceLifecycleService) CancelInTransaction(ctx context.Context, invoiceID id.ID) (bool, error) {
	invoice, err := s.invoices.GetByIDForUpdate(ctx, invoiceID)
	if err != nil {
		return false, fmt.Errorf("lock invoice %s for cancellation: %w", invoiceID, err)
	}
	if invoice.Status == crypto_invoice.InvoiceStatusCancelled {
		return false, nil
	}
	switch invoice.Status {
	case crypto_invoice.InvoiceStatusCreated, crypto_invoice.InvoiceStatusPartiallyPaid, crypto_invoice.InvoiceStatusPaid:
	default:
		return false, fmt.Errorf("%w from status %s", ErrInvoiceNotCancellable, invoice.Status)
	}

	summary, err := s.payments.SummarizeByInvoice(ctx, invoiceID)
	if err != nil {
		return false, fmt.Errorf("summarize cancelling invoice %s payments: %w", invoiceID, err)
	}
	if summary.ConfirmedAmount.Cmp(invoice.ExpectedAmount) >= 0 || summary.CreditedAmount.IsPositive() {
		return false, fmt.Errorf("%w after payment finality", ErrInvoiceNotCancellable)
	}
	invoice.ReceivedAmount = summary.ReceivedAmount
	invoice.ConfirmedAmount = summary.ConfirmedAmount
	invoice.CreditedAmount = summary.CreditedAmount
	if summary.ReceivedAmount.Cmp(invoice.ExpectedAmount) > 0 {
		invoice.OverpaidAmount = summary.ReceivedAmount.Sub(invoice.ExpectedAmount)
	} else {
		invoice.OverpaidAmount = types.ZeroRawAmount()
	}
	invoice.Status = crypto_invoice.InvoiceStatusCancelled
	if err := s.invoices.Update(ctx, invoice); err != nil {
		return false, err
	}
	if err := s.publish(ctx, WebhookInvoiceCancelled, invoice); err != nil {
		return false, err
	}
	return true, nil
}

func (s *InvoiceLifecycleService) saveIfProjectionChanged(ctx context.Context, invoice *crypto_invoice.CryptoInvoice, oldReceived, oldConfirmed, oldCredited, oldOverpaid types.RawAmount, oldStatus crypto_invoice.InvoiceStatus) error {
	if invoice.ReceivedAmount.Equal(oldReceived) && invoice.ConfirmedAmount.Equal(oldConfirmed) &&
		invoice.CreditedAmount.Equal(oldCredited) && invoice.OverpaidAmount.Equal(oldOverpaid) && invoice.Status == oldStatus {
		return nil
	}
	return s.invoices.Update(ctx, invoice)
}

func (s *InvoiceLifecycleService) publish(ctx context.Context, eventType WebhookEventType, invoice *crypto_invoice.CryptoInvoice) error {
	return s.publishWithExtra(ctx, eventType, invoice, nil)
}

func (s *InvoiceLifecycleService) publishWithExtra(ctx context.Context, eventType WebhookEventType, invoice *crypto_invoice.CryptoInvoice, extra map[string]any) error {
	if s.publisher == nil {
		return nil
	}
	invoiceID := invoice.ID
	return s.publisher.PublishLifecycleWebhook(ctx, LifecycleWebhookEvent{
		Event: eventType, AggregateID: invoice.ID, MerchantID: invoice.MerchantID,
		InvoiceID: &invoiceID, ResourceVersion: invoice.Version,
		OccurredAt: s.now().UTC(), Data: InvoiceWebhookData(invoice, extra),
	})
}
