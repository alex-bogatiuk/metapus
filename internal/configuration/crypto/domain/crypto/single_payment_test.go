package crypto

import (
	"context"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
)

type singlePaymentInbox struct {
	ChainEventInbox
	rows  map[string]*InboundChainEvent
	order []string
}

func newSinglePaymentInbox() *singlePaymentInbox {
	return &singlePaymentInbox{rows: make(map[string]*InboundChainEvent)}
}

func (m *singlePaymentInbox) Upsert(_ context.Context, event InboundChainEvent) (InboundChainEvent, bool, error) {
	if existing := m.rows[event.TxHash]; existing != nil {
		return *existing, false, nil
	}
	event.ID = id.New()
	event.CreatedAt = time.Now().UTC()
	m.rows[event.TxHash] = &event
	m.order = append(m.order, event.TxHash)
	return event, true, nil
}

func (m *singlePaymentInbox) MarkMatched(_ context.Context, eventID, invoiceID, paymentID, allocationID id.ID) error {
	for _, row := range m.rows {
		if row.ID == eventID {
			row.MatchStatus = InboxMatched
			row.InvoiceID = &invoiceID
			row.PaymentID = &paymentID
		}
	}
	return nil
}

func (m *singlePaymentInbox) MarkSinglePaymentReview(_ context.Context, eventID, invoiceID, allocationID id.ID) error {
	for _, row := range m.rows {
		if row.ID == eventID {
			row.MatchStatus = InboxNeedsReview
			row.MatchReason = MatchReasonAdditionalSinglePayment
			row.InvoiceID = &invoiceID
		}
	}
	return nil
}

func (m *singlePaymentInbox) NextSinglePaymentReview(_ context.Context, invoiceID id.ID) (*InboundChainEvent, error) {
	for _, key := range m.order {
		row := m.rows[key]
		if row.InvoiceID != nil && *row.InvoiceID == invoiceID &&
			row.MatchStatus == InboxNeedsReview && row.Canonicality == string(CanonicalityCanonical) {
			return row, nil
		}
	}
	return nil, nil
}

func (m *singlePaymentInbox) UpdateReviewChainState(_ context.Context, event BlockchainEvent) error {
	if row := m.rows[event.TxHash]; row != nil && row.MatchStatus == InboxNeedsReview {
		row.Confirmations = event.Confirmations
		row.FinalityStatus = string(event.Finality)
		if event.EventType == EventTypeReorg {
			row.Canonicality = string(CanonicalityOrphaned)
		} else if event.Canonicality != "" {
			row.Canonicality = string(event.Canonicality)
		}
	}
	return nil
}

func TestSinglePayment_AdditionalTransferHeldAndPromotedAfterReorg(t *testing.T) {
	ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})
	f := setupTestFixture(t, 20_000_000)
	f.invoiceRepo.invoices[f.invoiceID].SinglePayment = true
	inbox := newSinglePaymentInbox()
	f.processor.inbox = inbox

	first := f.makeTransferEvent(4_000_000)
	first.Canonicality = CanonicalityCanonical
	second := f.makeTransferEvent(5_000_000)
	second.Canonicality = CanonicalityCanonical
	third := f.makeTransferEvent(5_000_000)
	third.Canonicality = CanonicalityCanonical
	for _, event := range []BlockchainEvent{first, second, second, third} {
		if err := f.processor.ProcessEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.paymentRepo.payments) != 1 || inbox.rows[second.TxHash].MatchStatus != InboxNeedsReview {
		t.Fatal("additional transfer created a payment or was not held for review")
	}
	if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(third.TxHash, 19)); err != nil {
		t.Fatal(err)
	}
	if len(f.paymentRepo.payments) != 1 {
		t.Fatal("held transfer was credited on confirmation")
	}
	if err := f.processor.ProcessEvent(ctx, BlockchainEvent{
		NetworkID: f.networkID, TxHash: second.TxHash, EventKind: EventKindContractLog,
		EventType: EventTypeReorg, Canonicality: CanonicalityOrphaned,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.processor.ProcessEvent(ctx, BlockchainEvent{
		NetworkID: f.networkID, TxHash: first.TxHash, EventKind: EventKindContractLog,
		EventType: EventTypeReorg, Canonicality: CanonicalityOrphaned,
	}); err != nil {
		t.Fatal(err)
	}
	selected := f.invoiceRepo.invoices[f.invoiceID].SelectedPaymentID
	if selected == nil || f.paymentRepo.payments[*selected].TxHash != third.TxHash {
		t.Fatal("earliest canonical held transfer was not promoted")
	}
	if inbox.rows[second.TxHash].MatchStatus != InboxNeedsReview || inbox.rows[third.TxHash].MatchStatus != InboxMatched {
		t.Fatal("promoted transfer was not marked matched")
	}
	if f.invoiceRepo.invoices[f.invoiceID].Status != crypto_invoice.InvoiceStatusCompleted {
		t.Fatal("promoted finalized transfer did not complete invoice")
	}
}

func TestSinglePayment_AdditionalTransferAfterFinalityStaysInOperatorQueue(t *testing.T) {
	ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})
	f := setupTestFixture(t, 20_000_000)
	f.invoiceRepo.invoices[f.invoiceID].SinglePayment = true
	inbox := newSinglePaymentInbox()
	f.processor.inbox = inbox
	allocations := &memAllocRepo{rows: []WalletAllocation{{
		ID: id.New(), WalletID: f.walletID, InvoiceID: f.invoiceID,
		MerchantID: f.merchantID, TokenID: f.tokenID, NetworkID: f.networkID,
		LeasedAt: time.Now().Add(-time.Minute),
	}}}
	f.processor.allocations = allocations
	first := f.makeTransferEvent(4_000_000)
	first.Canonicality = CanonicalityCanonical
	if err := f.processor.ProcessEvent(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(first.TxHash, 19)); err != nil {
		t.Fatal(err)
	}
	if f.invoiceRepo.invoices[f.invoiceID].Status != crypto_invoice.InvoiceStatusUnderpaid {
		t.Fatal("first payment did not close invoice as underpaid")
	}
	now := time.Now().UTC()
	_, _ = allocations.CloseOpenForWallet(ctx, f.walletID, AllocationCloseConfirmed, now, now.Add(time.Hour))
	extra := f.makeTransferEvent(5_000_000)
	extra.Canonicality = CanonicalityCanonical
	if err := f.processor.ProcessEvent(ctx, extra); err != nil {
		t.Fatal(err)
	}
	if len(f.paymentRepo.payments) != 1 || inbox.rows[extra.TxHash].MatchStatus != InboxNeedsReview {
		t.Fatal("additional transfer was credited or missing from operator queue")
	}
	if f.invoiceRepo.invoices[f.invoiceID].Status != crypto_invoice.InvoiceStatusUnderpaid {
		t.Fatal("additional transfer reopened invoice")
	}
}

type singlePaymentReorgRecorder struct{ calls int }

func (r *singlePaymentReorgRecorder) RecordFinancialReorg(context.Context, *crypto_payment.CryptoPayment, BlockchainEvent) error {
	r.calls++
	return nil
}

func TestSinglePayment_PostFinalityReorgKeepsInvoiceClosed(t *testing.T) {
	ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})
	f := setupTestFixture(t, 20_000_000)
	f.invoiceRepo.invoices[f.invoiceID].SinglePayment = true
	recorder := &singlePaymentReorgRecorder{}
	f.processor.financialReorgs = recorder
	first := f.makeTransferEvent(4_000_000)
	first.Canonicality = CanonicalityCanonical
	if err := f.processor.ProcessEvent(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(first.TxHash, 19)); err != nil {
		t.Fatal(err)
	}
	selected := f.invoiceRepo.invoices[f.invoiceID].SelectedPaymentID
	if err := f.processor.ProcessEvent(ctx, BlockchainEvent{
		NetworkID: f.networkID, TxHash: first.TxHash, EventKind: EventKindContractLog,
		EventType: EventTypeReorg, Canonicality: CanonicalityOrphaned,
	}); err != nil {
		t.Fatal(err)
	}
	invoice := f.invoiceRepo.invoices[f.invoiceID]
	if invoice.Status != crypto_invoice.InvoiceStatusUnderpaid || invoice.SelectedPaymentID == nil ||
		*invoice.SelectedPaymentID != *selected || recorder.calls != 1 {
		t.Fatalf("post-finality reorg reopened invoice or skipped incident: status=%s incidents=%d", invoice.Status, recorder.calls)
	}
}
