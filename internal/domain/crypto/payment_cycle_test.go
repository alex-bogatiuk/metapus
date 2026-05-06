package crypto

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_payment"
)

// ── In-memory mocks ─────────────────────────────────────────────────────

// noopTxManager executes fn directly without a real DB transaction.
type noopTxManager struct{}

func (n *noopTxManager) RunInTransaction(_ context.Context, fn func(context.Context) error) error {
	return fn(context.Background())
}

// memPaymentEventRepo stores FSM audit events in memory.
type memPaymentEventRepo struct {
	events []PaymentEvent
}

func (r *memPaymentEventRepo) Create(_ context.Context, e *PaymentEvent) error {
	r.events = append(r.events, *e)
	return nil
}
func (r *memPaymentEventRepo) GetByPaymentID(_ context.Context, paymentID id.ID) ([]PaymentEvent, error) {
	var out []PaymentEvent
	for _, e := range r.events {
		if e.PaymentID == paymentID {
			out = append(out, e)
		}
	}
	return out, nil
}

// memInvoiceRepo stores crypto invoices in memory.
type memInvoiceRepo struct {
	invoices map[id.ID]*crypto_invoice.CryptoInvoice
}

func newMemInvoiceRepo() *memInvoiceRepo {
	return &memInvoiceRepo{invoices: make(map[id.ID]*crypto_invoice.CryptoInvoice)}
}
func (r *memInvoiceRepo) GetByID(_ context.Context, docID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	inv, ok := r.invoices[docID]
	if !ok {
		return nil, fmt.Errorf("invoice %s not found", docID)
	}
	return inv, nil
}
func (r *memInvoiceRepo) Update(_ context.Context, doc *crypto_invoice.CryptoInvoice) error {
	r.invoices[doc.ID] = doc
	return nil
}

// Stubs — not called in this test flow
func (r *memInvoiceRepo) Create(_ context.Context, _ *crypto_invoice.CryptoInvoice) error { return nil }
func (r *memInvoiceRepo) GetByNumber(_ context.Context, _ string) (*crypto_invoice.CryptoInvoice, error) {
	return nil, fmt.Errorf("not implemented")
}
func (r *memInvoiceRepo) Delete(_ context.Context, _ id.ID) error { return nil }
func (r *memInvoiceRepo) GetLines(_ context.Context, _ id.ID) ([]crypto_invoice.CryptoInvoiceLine, error) {
	return nil, nil
}
func (r *memInvoiceRepo) SaveLines(_ context.Context, _ id.ID, _ []crypto_invoice.CryptoInvoiceLine) error {
	return nil
}
func (r *memInvoiceRepo) List(_ context.Context, _ domain.ListFilter) (domain.CursorListResult[*crypto_invoice.CryptoInvoice], error) {
	return domain.CursorListResult[*crypto_invoice.CryptoInvoice]{}, nil
}
func (r *memInvoiceRepo) ListIDs(_ context.Context, _ domain.ListFilter, _ int) ([]id.ID, error) {
	return nil, nil
}
func (r *memInvoiceRepo) GetForUpdate(_ context.Context, docID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return r.GetByID(context.Background(), docID)
}
func (r *memInvoiceRepo) FindByExternalID(_ context.Context, _ string) (*crypto_invoice.CryptoInvoice, error) {
	return nil, fmt.Errorf("not found")
}
func (r *memInvoiceRepo) ExpireOverdue(_ context.Context) (int64, error) { return 0, nil }

// memPaymentRepo stores crypto payments in memory.
type memPaymentRepo struct {
	payments map[id.ID]*crypto_payment.CryptoPayment
	byTxHash map[string]*crypto_payment.CryptoPayment
}

func newMemPaymentRepo() *memPaymentRepo {
	return &memPaymentRepo{
		payments: make(map[id.ID]*crypto_payment.CryptoPayment),
		byTxHash: make(map[string]*crypto_payment.CryptoPayment),
	}
}
func (r *memPaymentRepo) Create(_ context.Context, doc *crypto_payment.CryptoPayment) error {
	r.payments[doc.ID] = doc
	r.byTxHash[doc.TxHash] = doc
	return nil
}
func (r *memPaymentRepo) GetByID(_ context.Context, docID id.ID) (*crypto_payment.CryptoPayment, error) {
	p, ok := r.payments[docID]
	if !ok {
		return nil, fmt.Errorf("payment %s not found", docID)
	}
	return p, nil
}
func (r *memPaymentRepo) Update(_ context.Context, doc *crypto_payment.CryptoPayment) error {
	r.payments[doc.ID] = doc
	r.byTxHash[doc.TxHash] = doc
	return nil
}
func (r *memPaymentRepo) FindByTxHash(_ context.Context, txHash string) (*crypto_payment.CryptoPayment, error) {
	p, ok := r.byTxHash[txHash]
	if !ok {
		return nil, fmt.Errorf("payment for tx %s not found", txHash)
	}
	return p, nil
}

// Stubs
func (r *memPaymentRepo) GetByNumber(_ context.Context, _ string) (*crypto_payment.CryptoPayment, error) {
	return nil, fmt.Errorf("not implemented")
}
func (r *memPaymentRepo) Delete(_ context.Context, _ id.ID) error { return nil }
func (r *memPaymentRepo) GetLines(_ context.Context, _ id.ID) ([]crypto_payment.CryptoPaymentLine, error) {
	return nil, nil
}
func (r *memPaymentRepo) SaveLines(_ context.Context, _ id.ID, _ []crypto_payment.CryptoPaymentLine) error {
	return nil
}
func (r *memPaymentRepo) List(_ context.Context, _ domain.ListFilter) (domain.CursorListResult[*crypto_payment.CryptoPayment], error) {
	return domain.CursorListResult[*crypto_payment.CryptoPayment]{}, nil
}
func (r *memPaymentRepo) ListIDs(_ context.Context, _ domain.ListFilter, _ int) ([]id.ID, error) {
	return nil, nil
}
func (r *memPaymentRepo) GetForUpdate(_ context.Context, docID id.ID) (*crypto_payment.CryptoPayment, error) {
	return r.GetByID(context.Background(), docID)
}
func (r *memPaymentRepo) ListByStatus(_ context.Context, _ crypto_payment.PaymentStatus) ([]*crypto_payment.CryptoPayment, error) {
	return nil, nil
}

// memWalletRepo stores wallets in memory, implementing wallet.Repository.
type memWalletRepo struct {
	wallets   map[id.ID]*wallet.Wallet
	byAddress map[string]*wallet.Wallet // key = networkID:address
}

func newMemWalletRepo() *memWalletRepo {
	return &memWalletRepo{
		wallets:   make(map[id.ID]*wallet.Wallet),
		byAddress: make(map[string]*wallet.Wallet),
	}
}
func (r *memWalletRepo) addWallet(w *wallet.Wallet) {
	r.wallets[w.ID] = w
	r.byAddress[w.NetworkID.String()+":"+w.Address] = w
}
func (r *memWalletRepo) GetByID(_ context.Context, wid id.ID) (*wallet.Wallet, error) {
	w, ok := r.wallets[wid]
	if !ok {
		return nil, fmt.Errorf("wallet %s not found", wid)
	}
	return w, nil
}
func (r *memWalletRepo) Update(_ context.Context, w *wallet.Wallet) error {
	r.wallets[w.ID] = w
	r.byAddress[w.NetworkID.String()+":"+w.Address] = w
	return nil
}
func (r *memWalletRepo) FindByAddress(_ context.Context, networkID id.ID, address string) (*wallet.Wallet, error) {
	w, ok := r.byAddress[networkID.String()+":"+address]
	if !ok {
		return nil, fmt.Errorf("wallet for address %s not found", address)
	}
	return w, nil
}

// Stubs — CatalogRepository methods not called in this flow
func (r *memWalletRepo) Create(_ context.Context, _ *wallet.Wallet) error               { return nil }
func (r *memWalletRepo) GetByCode(_ context.Context, _ string) (*wallet.Wallet, error)   { return nil, fmt.Errorf("not impl") }
func (r *memWalletRepo) Delete(_ context.Context, _ id.ID) error                         { return nil }
func (r *memWalletRepo) SetDeletionMark(_ context.Context, _ id.ID, _ bool) error        { return nil }
func (r *memWalletRepo) Exists(_ context.Context, _ id.ID) (bool, error)                 { return false, nil }
func (r *memWalletRepo) ExistsByCode(_ context.Context, _ string) (bool, error)          { return false, nil }
func (r *memWalletRepo) GetTree(_ context.Context, _ *id.ID) ([]*wallet.Wallet, error)   { return nil, nil }
func (r *memWalletRepo) GetPath(_ context.Context, _ id.ID) ([]*wallet.Wallet, error)    { return nil, nil }
func (r *memWalletRepo) List(_ context.Context, _ domain.ListFilter) (domain.CursorListResult[*wallet.Wallet], error) {
	return domain.CursorListResult[*wallet.Wallet]{}, nil
}
func (r *memWalletRepo) LeaseForInvoice(_ context.Context, _, _ id.ID) (*wallet.Wallet, error) {
	return nil, fmt.Errorf("not impl")
}
func (r *memWalletRepo) CountFreeByNetwork(_ context.Context, _ id.ID) (int, error) { return 0, nil }

// ── Helpers ─────────────────────────────────────────────────────────────

type testFixture struct {
	processor     *EventProcessor
	walletRepo    *memWalletRepo
	invoiceRepo   *memInvoiceRepo
	paymentRepo   *memPaymentRepo
	eventRepo     *memPaymentEventRepo
	sweepResolver *SweepConfigResolver

	// Seeded entities
	networkID  id.ID
	merchantID id.ID
	tokenID    id.ID
	walletID   id.ID
	invoiceID  id.ID
	walletAddr string
}

func setupTestFixture(t *testing.T, sweepThreshold int64) *testFixture {
	t.Helper()

	networkID := id.New()
	merchantID := id.New()
	tokenID := id.New()
	walletID := id.New()
	invoiceID := id.New()
	walletAddr := "TTestWalletAddress123"

	// Seed wallet (leased for invoice)
	walletRepo := newMemWalletRepo()
	w := &wallet.Wallet{
		NetworkID:      networkID,
		Address:        walletAddr,
		DerivationPath: "m/44'/195'/0'/0/0",
	}
	w.ID = walletID
	w.Name = "Test Pool Wallet"
	w.Code = "W-TEST-001"
	w.Status = wallet.WalletStatusLeased
	w.AllocationMode = wallet.AllocationModeTransient
	w.Tier = wallet.WalletTierPool
	w.LeasedForID = &invoiceID
	w.IsActive = true
	walletRepo.addWallet(w)

	// Seed invoice (created, awaiting payment)
	invoiceRepo := newMemInvoiceRepo()
	inv := crypto_invoice.NewCryptoInvoice(merchantID, tokenID, types.NewCryptoAmount(big.NewInt(5_000_000)))
	inv.ID = invoiceID
	inv.WalletID = &walletID
	inv.ExpiresAt = time.Now().Add(30 * time.Minute)
	invoiceRepo.invoices[invoiceID] = inv

	// Payment + event repos
	paymentRepo := newMemPaymentRepo()
	eventRepo := &memPaymentEventRepo{}

	// FSM
	fsm := NewPaymentFSM(paymentRepo, eventRepo)

	// Wallet service (real service, mock repo)
	walletSvc := wallet.NewService(walletRepo, nil)

	// Sweep resolver (with threshold)
	tok := &token.Token{SweepThreshold: types.NewCryptoAmount(big.NewInt(sweepThreshold))}
	tok.ID = tokenID
	mockTokenRepo := &mockTokenRepo{tok: tok}
	sweepResolver := NewSweepConfigResolver(&mockMerchantConfigRepo{}, mockTokenRepo)

	// Build processor
	processor := NewEventProcessor(EventProcessorConfig{
		FSM:           fsm,
		WalletSvc:     walletSvc,
		InvoiceRepo:   invoiceRepo,
		PaymentRepo:   paymentRepo,
		TxManager:     &noopTxManager{},
		SweepResolver: sweepResolver,
	})

	return &testFixture{
		processor:   processor,
		walletRepo:  walletRepo,
		invoiceRepo: invoiceRepo,
		paymentRepo: paymentRepo,
		eventRepo:   eventRepo,
		networkID:   networkID,
		merchantID:  merchantID,
		tokenID:     tokenID,
		walletID:    walletID,
		invoiceID:   invoiceID,
		walletAddr:  walletAddr,
	}
}

func (f *testFixture) makeTransferEvent(amount int64) BlockchainEvent {
	return BlockchainEvent{
		EventType:     EventTypeTransfer,
		NetworkID:     f.networkID,
		TxHash:        "0xtesthash_" + id.New().String()[:8],
		ToAddress:     f.walletAddr,
		FromAddress:   "TSenderAddress",
		Amount:        types.NewCryptoAmount(big.NewInt(amount)),
		BlockNumber:   100,
		Confirmations: 0,
		RequiredConfs: 19,
		TokenContract: "TTokenContract",
		Timestamp:     time.Now().UTC(),
	}
}

func (f *testFixture) makeConfirmationEvent(txHash string, confs int) BlockchainEvent {
	return BlockchainEvent{
		EventType:     EventTypeConfirmation,
		NetworkID:     f.networkID,
		TxHash:        txHash,
		ToAddress:     f.walletAddr,
		Amount:        types.NewCryptoAmount(big.NewInt(5_000_000)),
		BlockNumber:   int64(100 + confs),
		Confirmations: confs,
		RequiredConfs: 19,
		Timestamp:     time.Now().UTC(),
	}
}

// ── E2E Tests ───────────────────────────────────────────────────────────

// TestPaymentCycle_FullFlow tests the complete payment lifecycle:
// Transfer → Detected → Confirming → Confirmed → Invoice Confirmed → Wallet Released
func TestPaymentCycle_FullFlow(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000) // threshold > payment → wallet released (not swept)

	// ── Step 1: Transfer detected (0 confirmations) ────────────────────
	event := f.makeTransferEvent(5_000_000) // 5 USDT
	txHash := event.TxHash

	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("Step 1 (transfer): %v", err)
	}

	// Verify: payment created in Detected status
	payment, err := f.paymentRepo.FindByTxHash(ctx, txHash)
	if err != nil {
		t.Fatalf("payment not created: %v", err)
	}
	if payment.Status != crypto_payment.PaymentStatusDetected {
		t.Errorf("payment status = %q, want %q", payment.Status, crypto_payment.PaymentStatusDetected)
	}
	if payment.Amount.BigInt().Int64() != 5_000_000 {
		t.Errorf("payment amount = %s, want 5000000", payment.Amount.String())
	}

	// Verify: invoice updated to PartiallyPaid (received < expected? No, 5M == 5M → Paid)
	inv, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if inv.Status != crypto_invoice.InvoiceStatusPaid {
		t.Errorf("invoice status = %q, want %q", inv.Status, crypto_invoice.InvoiceStatusPaid)
	}
	if inv.ReceivedAmount.BigInt().Int64() != 5_000_000 {
		t.Errorf("invoice receivedAmount = %s, want 5000000", inv.ReceivedAmount.String())
	}

	// ── Step 2: First confirmation (1/19) ──────────────────────────────
	confEvent := f.makeConfirmationEvent(txHash, 1)
	if err := f.processor.ProcessEvent(ctx, confEvent); err != nil {
		t.Fatalf("Step 2 (1st confirmation): %v", err)
	}

	payment, _ = f.paymentRepo.FindByTxHash(ctx, txHash)
	if payment.Status != crypto_payment.PaymentStatusConfirming {
		t.Errorf("after 1 conf: status = %q, want %q", payment.Status, crypto_payment.PaymentStatusConfirming)
	}
	if payment.Confirmations != 1 {
		t.Errorf("confirmations = %d, want 1", payment.Confirmations)
	}

	// ── Step 3: Partial confirmations (10/19) ──────────────────────────
	confEvent = f.makeConfirmationEvent(txHash, 10)
	if err := f.processor.ProcessEvent(ctx, confEvent); err != nil {
		t.Fatalf("Step 3 (10 confirmations): %v", err)
	}

	payment, _ = f.paymentRepo.FindByTxHash(ctx, txHash)
	if payment.Status != crypto_payment.PaymentStatusConfirming {
		t.Errorf("after 10 confs: status = %q, want %q", payment.Status, crypto_payment.PaymentStatusConfirming)
	}
	if payment.Confirmations != 10 {
		t.Errorf("confirmations = %d, want 10", payment.Confirmations)
	}

	// ── Step 4: Final confirmation (19/19) → Confirmed ─────────────────
	confEvent = f.makeConfirmationEvent(txHash, 19)
	if err := f.processor.ProcessEvent(ctx, confEvent); err != nil {
		t.Fatalf("Step 4 (19 confirmations): %v", err)
	}

	payment, _ = f.paymentRepo.FindByTxHash(ctx, txHash)
	if payment.Status != crypto_payment.PaymentStatusConfirmed {
		t.Errorf("after 19 confs: status = %q, want %q", payment.Status, crypto_payment.PaymentStatusConfirmed)
	}
	if !payment.Posted {
		t.Error("payment should be Posted after confirmation")
	}
	if payment.PostedVersion != 1 {
		t.Errorf("postedVersion = %d, want 1", payment.PostedVersion)
	}
	if payment.ConfirmedAt == nil {
		t.Error("confirmedAt should be set")
	}

	// Verify: invoice confirmed
	inv, _ = f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if inv.Status != crypto_invoice.InvoiceStatusConfirmed {
		t.Errorf("invoice status = %q, want %q", inv.Status, crypto_invoice.InvoiceStatusConfirmed)
	}

	// Verify: wallet released (threshold > payment → transient wallet released to free)
	w, _ := f.walletRepo.GetByID(ctx, f.walletID)
	if w.Status != wallet.WalletStatusFree {
		t.Errorf("wallet status = %q, want %q (threshold mode: release transient)", w.Status, wallet.WalletStatusFree)
	}

	// Verify: FSM audit trail
	events, _ := f.eventRepo.GetByPaymentID(ctx, payment.ID)
	if len(events) != 2 {
		t.Fatalf("expected 2 FSM events, got %d", len(events))
	}
	if events[0].FromStatus != crypto_payment.PaymentStatusDetected ||
		events[0].ToStatus != crypto_payment.PaymentStatusConfirming {
		t.Errorf("event[0]: %s→%s, want detected→confirming", events[0].FromStatus, events[0].ToStatus)
	}
	if events[1].FromStatus != crypto_payment.PaymentStatusConfirming ||
		events[1].ToStatus != crypto_payment.PaymentStatusConfirmed {
		t.Errorf("event[1]: %s→%s, want confirming→confirmed", events[1].FromStatus, events[1].ToStatus)
	}
}

// TestPaymentCycle_ZeroThreshold_ImmediateSweep verifies legacy behavior:
// threshold=0 → MarkSweepPending after confirmation (not Release).
func TestPaymentCycle_ZeroThreshold_ImmediateSweep(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 0) // zero threshold → immediate sweep

	// Transfer with enough confirmations to go Detected → Confirming → Confirmed in one event
	event := f.makeTransferEvent(5_000_000)
	event.Confirmations = 19 // already fully confirmed
	txHash := event.TxHash

	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// Verify: payment went straight to Confirmed
	payment, _ := f.paymentRepo.FindByTxHash(ctx, txHash)
	if payment.Status != crypto_payment.PaymentStatusConfirmed {
		t.Errorf("status = %q, want confirmed", payment.Status)
	}

	// Verify: wallet marked sweep_pending (NOT free)
	w, _ := f.walletRepo.GetByID(ctx, f.walletID)
	if w.Status != wallet.WalletStatusSweepPending {
		t.Errorf("wallet status = %q, want %q (zero threshold → immediate sweep)", w.Status, wallet.WalletStatusSweepPending)
	}
}

// TestPaymentCycle_DustRejection verifies that sub-threshold amounts are ignored.
func TestPaymentCycle_DustRejection(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)

	// Send dust amount (999 < default dust threshold 1000)
	event := f.makeTransferEvent(999)
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// Verify: no payment created
	_, err := f.paymentRepo.FindByTxHash(ctx, event.TxHash)
	if err == nil {
		t.Error("dust payment should NOT be created")
	}
}

// TestPaymentCycle_Idempotency verifies that processing the same tx twice doesn't duplicate.
func TestPaymentCycle_Idempotency(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)

	event := f.makeTransferEvent(5_000_000)
	txHash := event.TxHash

	// First processing → creates payment
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("1st ProcessEvent: %v", err)
	}

	// Second processing → should update confirmations, not create duplicate
	event.Confirmations = 5
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("2nd ProcessEvent: %v", err)
	}

	// Verify: still one payment, confirmations updated
	payment, _ := f.paymentRepo.FindByTxHash(ctx, txHash)
	if payment.Confirmations != 5 {
		t.Errorf("confirmations = %d, want 5", payment.Confirmations)
	}
	if len(f.paymentRepo.payments) != 1 {
		t.Errorf("expected 1 payment, got %d (idempotency violated)", len(f.paymentRepo.payments))
	}
}

// TestPaymentCycle_ExpiredInvoice verifies that payments to expired invoices are rejected.
func TestPaymentCycle_ExpiredInvoice(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)

	// Expire the invoice
	inv, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	inv.ExpiresAt = time.Now().Add(-1 * time.Hour)
	f.invoiceRepo.invoices[f.invoiceID] = inv

	event := f.makeTransferEvent(5_000_000)
	// Should not error — just silently skip (no active invoice)
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// Verify: no payment created
	_, err := f.paymentRepo.FindByTxHash(ctx, event.TxHash)
	if err == nil {
		t.Error("payment should NOT be created for expired invoice")
	}

	// Verify: invoice marked expired
	inv, _ = f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if inv.Status != crypto_invoice.InvoiceStatusExpired {
		t.Errorf("invoice status = %q, want expired", inv.Status)
	}
}

// TestPaymentCycle_UnknownWallet verifies events for unknown addresses are silently skipped.
func TestPaymentCycle_UnknownWallet(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)

	event := f.makeTransferEvent(5_000_000)
	event.ToAddress = "TUnknownAddress999"

	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// No payment should be created
	if len(f.paymentRepo.payments) != 0 {
		t.Errorf("expected 0 payments for unknown wallet, got %d", len(f.paymentRepo.payments))
	}
}
