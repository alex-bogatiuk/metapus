package crypto

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/posting"
)

// ── In-memory mocks ─────────────────────────────────────────────────────

// noopTxManager executes fn directly without a real DB transaction.
type noopTxManager struct{}

func (n *noopTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
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
func (r *memInvoiceRepo) GetByIDForUpdate(ctx context.Context, docID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return r.GetByID(ctx, docID)
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
func (r *memInvoiceRepo) List(_ context.Context, _ domain.ListFilter) (domain.CursorListResult[*crypto_invoice.CryptoInvoice], error) {
	return domain.CursorListResult[*crypto_invoice.CryptoInvoice]{}, nil
}
func (r *memInvoiceRepo) ListIDs(_ context.Context, _ domain.ListFilter, _ int) ([]id.ID, error) {
	return nil, nil
}
func (r *memInvoiceRepo) FindByExternalID(_ context.Context, _ string) (*crypto_invoice.CryptoInvoice, error) {
	return nil, nil
}
func (r *memInvoiceRepo) FindByPublicRef(_ context.Context, publicRef string) (*crypto_invoice.CryptoInvoice, error) {
	for _, inv := range r.invoices {
		if inv.PublicRef == publicRef {
			return inv, nil
		}
	}
	return nil, nil
}
func (r *memInvoiceRepo) FindByMerchantOrderID(_ context.Context, merchantID id.ID, orderID string) (*crypto_invoice.CryptoInvoice, error) {
	for _, inv := range r.invoices {
		if inv.MerchantID == merchantID && inv.OrderID == orderID {
			return inv, nil
		}
	}
	return nil, nil
}
func (r *memInvoiceRepo) FindByMerchantExternalID(_ context.Context, merchantID id.ID, externalID string) (*crypto_invoice.CryptoInvoice, error) {
	for _, inv := range r.invoices {
		if inv.MerchantID == merchantID && inv.ExternalID == externalID {
			return inv, nil
		}
	}
	return nil, nil
}
func (r *memInvoiceRepo) FindOverdueIDs(_ context.Context, _ int) ([]id.ID, error)  { return nil, nil }
func (r *memInvoiceRepo) LockWalletForInvoice(context.Context, id.ID) error         { return nil }
func (r *memInvoiceRepo) ReleaseInvoiceWallet(context.Context, id.ID, string) error { return nil }
func (r *memInvoiceRepo) CleanupOrphanedExpiredLeases(context.Context) error        { return nil }

// memPaymentRepo stores crypto payments in memory.
type memPaymentRepo struct {
	payments        map[id.ID]*crypto_payment.CryptoPayment
	byTxHash        map[string]*crypto_payment.CryptoPayment
	findByTxHashErr error
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
	if r.findByTxHashErr != nil {
		return nil, r.findByTxHashErr
	}
	p, ok := r.byTxHash[txHash]
	if !ok {
		return nil, nil
	}
	return p, nil
}
func (r *memPaymentRepo) FindByChainEvent(_ context.Context, networkID id.ID, txHash, eventKind string, eventIndex int64) (*crypto_payment.CryptoPayment, error) {
	if r.findByTxHashErr != nil {
		return nil, r.findByTxHashErr
	}
	payment := r.byTxHash[txHash]
	if payment == nil || payment.NetworkID != networkID || payment.EventKind != eventKind || payment.EventIndex != eventIndex {
		return nil, nil
	}
	return payment, nil
}
func (r *memPaymentRepo) FindBySenderNonce(_ context.Context, networkID id.ID, sourceAddress string, sourceNonce int64) (*crypto_payment.CryptoPayment, error) {
	for _, payment := range r.payments {
		if payment.NetworkID == networkID && payment.SourceAddress == sourceAddress && payment.SourceNonce != nil && *payment.SourceNonce == sourceNonce && payment.Status != crypto_payment.PaymentStatusFailed {
			return payment, nil
		}
	}
	return nil, nil
}
func (r *memPaymentRepo) SummarizeByInvoice(_ context.Context, invoiceID id.ID) (crypto_payment.InvoicePaymentSummary, error) {
	summary := crypto_payment.InvoicePaymentSummary{
		ReceivedAmount:  types.ZeroRawAmount(),
		ConfirmedAmount: types.ZeroRawAmount(),
		CreditedAmount:  types.ZeroRawAmount(),
	}
	for _, payment := range r.payments {
		if payment.InvoiceID != invoiceID || payment.DeletionMark || payment.Canonicality != string(CanonicalityCanonical) {
			continue
		}
		switch payment.Status {
		case crypto_payment.PaymentStatusDetected, crypto_payment.PaymentStatusConfirming,
			crypto_payment.PaymentStatusConfirmed, crypto_payment.PaymentStatusSettled:
			summary.ReceivedAmount = summary.ReceivedAmount.Add(payment.Amount)
		}
		switch payment.Status {
		case crypto_payment.PaymentStatusConfirmed, crypto_payment.PaymentStatusSettled:
			summary.ConfirmedAmount = summary.ConfirmedAmount.Add(payment.Amount)
			if payment.Posted {
				summary.CreditedAmount = summary.CreditedAmount.Add(payment.Amount)
			}
		}
	}
	return summary, nil
}

// Stubs
func (r *memPaymentRepo) GetByNumber(_ context.Context, _ string) (*crypto_payment.CryptoPayment, error) {
	return nil, fmt.Errorf("not implemented")
}
func (r *memPaymentRepo) Delete(_ context.Context, _ id.ID) error { return nil }
func (r *memPaymentRepo) List(_ context.Context, _ domain.ListFilter) (domain.CursorListResult[*crypto_payment.CryptoPayment], error) {
	return domain.CursorListResult[*crypto_payment.CryptoPayment]{}, nil
}
func (r *memPaymentRepo) ListIDs(_ context.Context, _ domain.ListFilter, _ int) ([]id.ID, error) {
	return nil, nil
}
func (r *memPaymentRepo) ClaimForConfirmation(_ context.Context, _ []crypto_payment.PaymentStatus, _ string, _ int, _ time.Duration) ([]*crypto_payment.CryptoPayment, error) {
	return nil, nil
}
func (r *memPaymentRepo) ReleaseConfirmationClaim(_ context.Context, _ id.ID, _ string) error {
	return nil
}
func (r *memPaymentRepo) GetByIDForUpdate(_ context.Context, docID id.ID) (*crypto_payment.CryptoPayment, error) {
	// In-memory: no locking needed, just delegate to GetByID.
	p, ok := r.payments[docID]
	if !ok {
		return nil, fmt.Errorf("payment %s not found", docID)
	}
	return p, nil
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
		return nil, apperror.NewNotFound("wallet", address)
	}
	return w, nil
}

// Stubs — CatalogRepository methods not called in this flow
func (r *memWalletRepo) Create(_ context.Context, _ *wallet.Wallet) error { return nil }
func (r *memWalletRepo) GetByCode(_ context.Context, _ string) (*wallet.Wallet, error) {
	return nil, fmt.Errorf("not impl")
}
func (r *memWalletRepo) Delete(_ context.Context, _ id.ID) error                  { return nil }
func (r *memWalletRepo) SetDeletionMark(_ context.Context, _ id.ID, _ bool) error { return nil }
func (r *memWalletRepo) Exists(_ context.Context, _ id.ID) (bool, error)          { return false, nil }
func (r *memWalletRepo) ExistsByCode(_ context.Context, _ string) (bool, error)   { return false, nil }
func (r *memWalletRepo) GetTree(_ context.Context, _ *id.ID) ([]*wallet.Wallet, error) {
	return nil, nil
}
func (r *memWalletRepo) GetPath(_ context.Context, _ id.ID) ([]*wallet.Wallet, error) {
	return nil, nil
}
func (r *memWalletRepo) List(_ context.Context, _ domain.ListFilter) (domain.CursorListResult[*wallet.Wallet], error) {
	return domain.CursorListResult[*wallet.Wallet]{}, nil
}
func (r *memWalletRepo) LeaseForInvoice(_ context.Context, _, _, _, _ id.ID, _ time.Time) (*wallet.Wallet, error) {
	return nil, fmt.Errorf("not impl")
}
func (r *memWalletRepo) CountFreeByNetwork(_ context.Context, _ id.ID) (int, error) { return 0, nil }

func (r *memWalletRepo) FindPersistentByCustomerID(_ context.Context, _, _ id.ID, _ string) (*wallet.Wallet, error) {
	return nil, fmt.Errorf("not impl")
}

func (r *memWalletRepo) FindByAddressForUpdate(ctx context.Context, networkID id.ID, address string) (*wallet.Wallet, error) {
	return r.FindByAddress(ctx, networkID, address)
}
func (r *memWalletRepo) AssignPersistentAddress(_ context.Context, _, _ id.ID, _ string) (*wallet.Wallet, error) {
	return nil, fmt.Errorf("not impl")
}
func (r *memWalletRepo) BeginSweep(_ context.Context, _ id.ID) error { return nil }

// ── Helpers ─────────────────────────────────────────────────────────────

type testFixture struct {
	processor   *EventProcessor
	walletRepo  *memWalletRepo
	invoiceRepo *memInvoiceRepo
	paymentRepo *memPaymentRepo
	eventRepo   *memPaymentEventRepo
	webhooks    *memLifecycleWebhookPublisher
	// Seeded entities
	networkID  id.ID
	merchantID id.ID
	tokenID    id.ID
	walletID   id.ID
	invoiceID  id.ID
	walletAddr string
}

type memLifecycleWebhookPublisher struct {
	events []LifecycleWebhookEvent
	err    error
}

func (p *memLifecycleWebhookPublisher) PublishLifecycleWebhook(_ context.Context, event LifecycleWebhookEvent) error {
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, event)
	return nil
}

func (p *memLifecycleWebhookPublisher) has(eventType WebhookEventType) bool {
	for _, event := range p.events {
		if event.Event == eventType {
			return true
		}
	}
	return false
}

func (p *memLifecycleWebhookPublisher) count(eventType WebhookEventType) int {
	count := 0
	for _, event := range p.events {
		if event.Event == eventType {
			count++
		}
	}
	return count
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
	inv := crypto_invoice.NewCryptoInvoice(merchantID, tokenID, types.NewRawAmountFromInt64(5_000_000))
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
	tok := &token.Token{SweepThreshold: types.NewRawAmountFromInt64(sweepThreshold), DecimalPlaces: 6}
	tok.ID = tokenID
	mockTokenRepo := &mockTokenRepo{tok: tok}
	sweepResolver := NewSweepConfigResolver(&mockMerchantConfigRepo{}, mockTokenRepo)

	// Build processor
	webhooks := &memLifecycleWebhookPublisher{}
	processor := NewEventProcessor(EventProcessorConfig{
		FSM:              fsm,
		WalletSvc:        walletSvc,
		InvoiceSvc:       invoiceRepo,
		PaymentRepo:      paymentRepo,
		TxManager:        &noopTxManager{},
		SweepResolver:    sweepResolver,
		TokenResolver:    mockTokenRepo,
		PostingEngine:    posting.NewEngine(nil), // provide dummy engine
		Receipts:         &memPaymentReceipts{},
		WebhookPublisher: webhooks,
	})

	return &testFixture{
		processor:   processor,
		walletRepo:  walletRepo,
		invoiceRepo: invoiceRepo,
		paymentRepo: paymentRepo,
		eventRepo:   eventRepo,
		webhooks:    webhooks,
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
		TxHash:        "0xtesthash_" + id.Compact(id.New()),
		ToAddress:     f.walletAddr,
		FromAddress:   "TSenderAddress",
		Amount:        types.NewRawAmountFromInt64(amount),
		BlockNumber:   100,
		Finality:      FinalityProvisional,
		Confirmations: 0,
		RequiredConfs: 19,
		TokenContract: "TTokenContract",
		Timestamp:     time.Now().UTC(),
	}
}

func (f *testFixture) makeConfirmationEvent(txHash string, confs int) BlockchainEvent {
	amount := types.NewRawAmountFromInt64(5_000_000)
	for _, payment := range f.paymentRepo.payments {
		if payment.TxHash == txHash {
			amount = payment.Amount
			break
		}
	}
	finality := FinalityProvisional
	if confs >= 19 {
		finality = FinalitySolidified
	}
	return BlockchainEvent{
		EventType:     EventTypeConfirmation,
		NetworkID:     f.networkID,
		TxHash:        txHash,
		EventKind:     EventKindContractLog,
		ToAddress:     f.walletAddr,
		Amount:        amount,
		BlockNumber:   int64(100 + confs),
		Confirmations: confs,
		Finality:      finality,
		RequiredConfs: 19,
		Timestamp:     time.Now().UTC(),
	}
}

func TestPaymentCycle_ProvisionalReorgCompensatesAndReactivates(t *testing.T) {
	ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)
	event := f.makeTransferEvent(5_000_000)
	event.EventKind = EventKindContractLog
	event.Canonicality = CanonicalityCanonical

	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("detect transfer: %v", err)
	}
	if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(event.TxHash, 1)); err != nil {
		t.Fatalf("confirm transfer: %v", err)
	}
	beforeReorg, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if beforeReorg.ReceivedAmount.String() != "5000000" {
		t.Fatalf("invoice before reorg amount=%s, want 5000000", beforeReorg.ReceivedAmount)
	}
	payment, _ := f.paymentRepo.FindByTxHash(ctx, event.TxHash)
	paymentID := payment.ID

	reorg := f.makeConfirmationEvent(event.TxHash, 0)
	reorg.EventType = EventTypeReorg
	reorg.Canonicality = CanonicalityOrphaned
	if err := f.processor.ProcessEvent(ctx, reorg); err != nil {
		t.Fatalf("process reorg: %v", err)
	}
	payment, _ = f.paymentRepo.FindByTxHash(ctx, event.TxHash)
	if payment.Status != crypto_payment.PaymentStatusReorged || payment.Canonicality != string(CanonicalityOrphaned) {
		t.Fatalf("payment after reorg = %s/%s", payment.Status, payment.Canonicality)
	}
	invoice, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if !invoice.ReceivedAmount.IsZero() || invoice.Status != crypto_invoice.InvoiceStatusCreated {
		t.Fatalf("invoice was not compensated: amount=%s status=%s", invoice.ReceivedAmount, invoice.Status)
	}

	reappeared := event
	reappeared.Confirmations = 1
	if err := f.processor.ProcessEvent(ctx, reappeared); err != nil {
		t.Fatalf("reactivate canonical event: %v", err)
	}
	payment, _ = f.paymentRepo.FindByTxHash(ctx, event.TxHash)
	if payment.ID != paymentID || payment.Status != crypto_payment.PaymentStatusConfirming {
		t.Fatalf("reappearance created or left wrong payment: id=%s status=%s", payment.ID, payment.Status)
	}
	invoice, _ = f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if invoice.ReceivedAmount.String() != "5000000" || invoice.Status != crypto_invoice.InvoiceStatusPaid {
		t.Fatalf("invoice was not restored exactly once: amount=%s status=%s", invoice.ReceivedAmount, invoice.Status)
	}
	if len(f.paymentRepo.payments) != 1 {
		t.Fatalf("payment count=%d, want 1", len(f.paymentRepo.payments))
	}
}

// ── E2E Tests ───────────────────────────────────────────────────────────

// TestPaymentCycle_FullFlow tests the complete payment lifecycle:
// Transfer → Detected → Confirming → Confirmed → Invoice Confirmed → Wallet Released
func TestPaymentCycle_FullFlow(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
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
	if payment.Amount.String() != "5000000" {
		t.Errorf("payment amount = %s, want 5000000", payment.Amount.String())
	}

	// Verify: invoice updated to PartiallyPaid (received < expected? No, 5M == 5M → Paid)
	inv, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if inv.Status != crypto_invoice.InvoiceStatusPaid {
		t.Errorf("invoice status = %q, want %q", inv.Status, crypto_invoice.InvoiceStatusPaid)
	}
	if inv.ReceivedAmount.String() != "5000000" {
		t.Errorf("invoice receivedAmount = %s, want 5000000", inv.ReceivedAmount.String())
	}
	if !f.webhooks.has(WebhookPaymentDetected) || !f.webhooks.has(WebhookInvoicePaid) {
		t.Fatalf("expected payment.detected and invoice.paid lifecycle webhooks, got %#v", f.webhooks.events)
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
	if payment.Status != crypto_payment.PaymentStatusSettled {
		t.Errorf("after 19 confs: status = %q, want %q", payment.Status, crypto_payment.PaymentStatusSettled)
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

	// Verify: invoice finalized and credited to the merchant ledger.
	inv, _ = f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if inv.Status != crypto_invoice.InvoiceStatusCompleted {
		t.Errorf("invoice status = %q, want %q", inv.Status, crypto_invoice.InvoiceStatusCompleted)
	}
	if !f.webhooks.has(WebhookInvoiceConfirmed) || !f.webhooks.has(WebhookInvoiceCompleted) {
		t.Fatalf("expected invoice.confirmed and invoice.completed webhooks, got %#v", f.webhooks.events)
	}

	// Verify: wallet released (threshold > payment → transient wallet released to free)
	w, _ := f.walletRepo.GetByID(ctx, f.walletID)
	if w.Status != wallet.WalletStatusFree {
		t.Errorf("wallet status = %q, want %q (threshold mode: release transient)", w.Status, wallet.WalletStatusFree)
	}

	// Verify: FSM audit trail
	events, _ := f.eventRepo.GetByPaymentID(ctx, payment.ID)
	if len(events) != 3 {
		t.Fatalf("expected 3 FSM events, got %d", len(events))
	}
	if events[0].FromStatus != crypto_payment.PaymentStatusDetected ||
		events[0].ToStatus != crypto_payment.PaymentStatusConfirming {
		t.Errorf("event[0]: %s→%s, want detected→confirming", events[0].FromStatus, events[0].ToStatus)
	}
	if events[1].FromStatus != crypto_payment.PaymentStatusConfirming ||
		events[1].ToStatus != crypto_payment.PaymentStatusConfirmed {
		t.Errorf("event[1]: %s→%s, want confirming→confirmed", events[1].FromStatus, events[1].ToStatus)
	}
	if events[2].FromStatus != crypto_payment.PaymentStatusConfirmed ||
		events[2].ToStatus != crypto_payment.PaymentStatusSettled {
		t.Errorf("event[2]: %s→%s, want confirmed→settled", events[2].FromStatus, events[2].ToStatus)
	}
}

func TestPaymentCycle_DoesNotCompleteUntilCanonicalFinalizedSumCoversInvoice(t *testing.T) {
	ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})
	f := setupTestFixture(t, 20_000_000)
	invoice, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	invoice.ExpectedAmount = types.NewRawAmountFromInt64(10_000_000)

	first := f.makeTransferEvent(4_000_000)
	second := f.makeTransferEvent(6_000_000)
	if err := f.processor.ProcessEvent(ctx, first); err != nil {
		t.Fatalf("detect first partial payment: %v", err)
	}
	if err := f.processor.ProcessEvent(ctx, second); err != nil {
		t.Fatalf("detect second partial payment: %v", err)
	}

	invoice, _ = f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if invoice.Status != crypto_invoice.InvoiceStatusPaid || invoice.ReceivedAmount.String() != "10000000" {
		t.Fatalf("detected aggregate = %s/%s, want paid/10000000", invoice.Status, invoice.ReceivedAmount)
	}

	if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(first.TxHash, 19)); err != nil {
		t.Fatalf("finalize first partial payment: %v", err)
	}
	invoice, _ = f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if invoice.Status == crypto_invoice.InvoiceStatusConfirmed || invoice.Status == crypto_invoice.InvoiceStatusCompleted {
		t.Fatalf("invoice advanced prematurely after one finalized partial payment: %s", invoice.Status)
	}
	if invoice.ConfirmedAmount.String() != "4000000" || invoice.CreditedAmount.String() != "4000000" {
		t.Fatalf("finalized projection = %s/%s, want 4000000/4000000", invoice.ConfirmedAmount, invoice.CreditedAmount)
	}

	if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(second.TxHash, 19)); err != nil {
		t.Fatalf("finalize second partial payment: %v", err)
	}
	invoice, _ = f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if invoice.Status != crypto_invoice.InvoiceStatusCompleted {
		t.Fatalf("invoice status = %s, want completed", invoice.Status)
	}
	if invoice.ConfirmedAmount.String() != "10000000" || invoice.CreditedAmount.String() != "10000000" {
		t.Fatalf("finalized projection = %s/%s, want 10000000/10000000", invoice.ConfirmedAmount, invoice.CreditedAmount)
	}
}

func TestPaymentCycle_SinglePaymentClosesAfterFirstFinality(t *testing.T) {
	for _, tc := range []struct {
		name   string
		amount int64
		status crypto_invoice.InvoiceStatus
	}{
		{name: "partial", amount: 4_000_000, status: crypto_invoice.InvoiceStatusUnderpaid},
		{name: "exact", amount: 5_000_000, status: crypto_invoice.InvoiceStatusCompleted},
		{name: "over", amount: 6_000_000, status: crypto_invoice.InvoiceStatusCompleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})
			f := setupTestFixture(t, 20_000_000)
			inv := f.invoiceRepo.invoices[f.invoiceID]
			inv.SinglePayment = true
			event := f.makeTransferEvent(tc.amount)
			event.Canonicality = CanonicalityCanonical
			if err := f.processor.ProcessEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			if inv.SelectedPaymentID == nil || inv.AcceptsPayments() {
				t.Fatal("first payment was not selected or invoice still accepts payments")
			}
			if inv.Status == crypto_invoice.InvoiceStatusUnderpaid {
				t.Fatal("invoice closed before finality")
			}
			if tc.status == crypto_invoice.InvoiceStatusUnderpaid {
				inv.ExpiresAt = time.Now().Add(-time.Minute)
				release, err := f.processor.ExpireInvoiceInTransaction(ctx, f.invoiceID)
				if err != nil || release || inv.Status == crypto_invoice.InvoiceStatusUnderpaid {
					t.Fatalf("pending selected payment expired before finality: release=%v status=%s err=%v", release, inv.Status, err)
				}
			}
			if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(event.TxHash, 19)); err != nil {
				t.Fatal(err)
			}
			if inv.Status != tc.status {
				t.Fatalf("invoice status = %s; want %s", inv.Status, tc.status)
			}
			if !f.webhooks.has(WebhookInvoiceUnderpaid) && tc.status == crypto_invoice.InvoiceStatusUnderpaid {
				t.Fatal("invoice.underpaid webhook missing")
			}
			w := f.walletRepo.wallets[f.walletID]
			if w.Status == wallet.WalletStatusLeased {
				t.Fatal("wallet lease was not released at finality")
			}
			if tc.status == crypto_invoice.InvoiceStatusUnderpaid {
				w.Status = wallet.WalletStatusLeased
				w.LeasedForID = &f.invoiceID
				if _, err := f.processor.ReconcileInvoice(ctx, f.invoiceID); err != nil {
					t.Fatal(err)
				}
				if w.Status == wallet.WalletStatusLeased || inv.Status != crypto_invoice.InvoiceStatusUnderpaid {
					t.Fatal("reconciliation did not preserve underpayment and release stale lease")
				}
			}
			if _, err := f.processor.ExpireInvoiceInTransaction(ctx, f.invoiceID); err != nil {
				t.Fatal(err)
			}
			if inv.Status != tc.status {
				t.Fatalf("TTL changed terminal status to %s", inv.Status)
			}
		})
	}
}

func TestInvoiceLifecycle_ReconcileCompletedInvoiceIsIdempotent(t *testing.T) {
	ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})
	f := setupTestFixture(t, 20_000_000)

	transfer := f.makeTransferEvent(10_000_000)
	if err := f.processor.ProcessEvent(ctx, transfer); err != nil {
		t.Fatalf("detect payment: %v", err)
	}
	if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(transfer.TxHash, 19)); err != nil {
		t.Fatalf("finalize payment: %v", err)
	}
	beforeEvents := len(f.webhooks.events)

	completed, err := f.processor.ReconcileInvoice(ctx, f.invoiceID)
	if err != nil {
		t.Fatalf("reconcile completed invoice: %v", err)
	}
	if completed {
		t.Fatal("idempotent reconciliation must not report a new completion")
	}
	invoice, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if invoice.Status != crypto_invoice.InvoiceStatusCompleted {
		t.Fatalf("invoice status after reconciliation = %s, want completed", invoice.Status)
	}
	if len(f.webhooks.events) != beforeEvents {
		t.Fatalf("idempotent reconciliation emitted %d new events", len(f.webhooks.events)-beforeEvents)
	}
}

func TestInvoiceLifecycle_ProvisionalOverpaymentDoesNotDowngradeCompleted(t *testing.T) {
	ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})
	f := setupTestFixture(t, 20_000_000)

	full := f.makeTransferEvent(5_000_000)
	if err := f.processor.ProcessEvent(ctx, full); err != nil {
		t.Fatalf("detect full payment: %v", err)
	}
	if err := f.processor.ProcessEvent(ctx, f.makeConfirmationEvent(full.TxHash, 19)); err != nil {
		t.Fatalf("finalize full payment: %v", err)
	}
	beforeCompleted := f.webhooks.count(WebhookInvoiceCompleted)

	overpayment := crypto_payment.NewCryptoPayment(
		f.invoiceID, f.merchantID, f.tokenID, f.walletID, f.networkID,
		"0xpost-completion-overpayment", 0, string(EventKindContractLog), "0xblock-overpayment", "0xsender",
		types.NewRawAmountFromInt64(1_000_000), 120, 19,
	)
	if err := f.paymentRepo.Create(ctx, overpayment); err != nil {
		t.Fatalf("persist overpayment: %v", err)
	}
	if _, err := f.processor.ReconcileInvoice(ctx, f.invoiceID); err != nil {
		t.Fatalf("reconcile overpayment: %v", err)
	}
	invoice, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if invoice.Status != crypto_invoice.InvoiceStatusCompleted {
		t.Fatalf("invoice status after provisional overpayment = %s, want completed", invoice.Status)
	}
	if invoice.OverpaidAmount.String() != "1000000" {
		t.Fatalf("overpaid amount = %s, want 1000000", invoice.OverpaidAmount)
	}
	if f.webhooks.count(WebhookInvoiceCompleted) != beforeCompleted {
		t.Fatal("provisional overpayment emitted a duplicate invoice.completed")
	}
	if !f.webhooks.has(WebhookInvoiceOverpaid) {
		t.Fatal("expected invoice.overpaid")
	}
}

func TestInvoiceLifecycle_ExpireWithPartialPaymentBecomesUnderpaid(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	invoice := f.invoiceRepo.invoices[f.invoiceID]
	invoice.ExpiresAt = now.Add(-time.Minute)

	payment := crypto_payment.NewCryptoPayment(
		f.invoiceID, f.merchantID, f.tokenID, f.walletID, f.networkID,
		"0xpartial-expiry", 0, string(EventKindContractLog), "0xblock", "0xsender",
		types.NewRawAmountFromInt64(2_000_000), 100, 19,
	)
	if err := f.paymentRepo.Create(ctx, payment); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewInvoiceLifecycleService(f.invoiceRepo, f.paymentRepo, f.webhooks, &noopTxManager{}, func() time.Time { return now })
	releaseWallet, err := lifecycle.ExpireInTransaction(ctx, f.invoiceID)
	if err != nil {
		t.Fatalf("expire invoice: %v", err)
	}
	if !releaseWallet {
		t.Fatal("underpaid terminal transition must release the invoice wallet")
	}
	if invoice.Status != crypto_invoice.InvoiceStatusUnderpaid {
		t.Fatalf("status = %s, want underpaid", invoice.Status)
	}
	if invoice.ReceivedAmount.String() != "2000000" || !invoice.ConfirmedAmount.IsZero() || !invoice.CreditedAmount.IsZero() {
		t.Fatalf("projection = received:%s confirmed:%s credited:%s", invoice.ReceivedAmount, invoice.ConfirmedAmount, invoice.CreditedAmount)
	}
	if len(f.webhooks.events) != 1 || f.webhooks.events[0].Event != WebhookInvoiceUnderpaid {
		t.Fatalf("events = %#v, want one invoice.underpaid", f.webhooks.events)
	}
	if got := f.webhooks.events[0].Data["underpaidAmount"]; got != "3000000" {
		t.Fatalf("underpaidAmount = %#v, want 3000000", got)
	}
}

func TestInvoiceLifecycle_ExpireWithoutPaymentBecomesExpired(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	invoice := f.invoiceRepo.invoices[f.invoiceID]
	invoice.ExpiresAt = now.Add(-time.Minute)

	lifecycle := NewInvoiceLifecycleService(f.invoiceRepo, f.paymentRepo, f.webhooks, &noopTxManager{}, func() time.Time { return now })
	releaseWallet, err := lifecycle.ExpireInTransaction(ctx, f.invoiceID)
	if err != nil {
		t.Fatalf("expire invoice: %v", err)
	}
	if !releaseWallet || invoice.Status != crypto_invoice.InvoiceStatusExpired {
		t.Fatalf("release/status = %t/%s, want true/expired", releaseWallet, invoice.Status)
	}
	if len(f.webhooks.events) != 1 || f.webhooks.events[0].Event != WebhookInvoiceExpired {
		t.Fatalf("events = %#v, want one invoice.expired", f.webhooks.events)
	}
}

func TestInvoiceLifecycle_FullPaymentWinsExpirationRace(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	invoice := f.invoiceRepo.invoices[f.invoiceID]
	invoice.ExpiresAt = now.Add(-time.Minute)

	payment := crypto_payment.NewCryptoPayment(
		f.invoiceID, f.merchantID, f.tokenID, f.walletID, f.networkID,
		"0xfull-expiry-race", 0, string(EventKindContractLog), "0xblock", "0xsender",
		invoice.ExpectedAmount, 100, 19,
	)
	if err := f.paymentRepo.Create(ctx, payment); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewInvoiceLifecycleService(f.invoiceRepo, f.paymentRepo, f.webhooks, &noopTxManager{}, func() time.Time { return now })
	releaseWallet, err := lifecycle.ExpireInTransaction(ctx, f.invoiceID)
	if err != nil {
		t.Fatalf("expire invoice: %v", err)
	}
	if releaseWallet {
		t.Fatal("a detected full payment must keep the wallet assigned while awaiting finality")
	}
	if invoice.Status != crypto_invoice.InvoiceStatusPaid {
		t.Fatalf("status = %s, want paid", invoice.Status)
	}
	if len(f.webhooks.events) != 1 || f.webhooks.events[0].Event != WebhookInvoicePaid {
		t.Fatalf("events = %#v, want one invoice.paid", f.webhooks.events)
	}
}

func TestInvoiceLifecycle_CancelEmitsEventAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	lifecycle := NewInvoiceLifecycleService(f.invoiceRepo, f.paymentRepo, f.webhooks, &noopTxManager{}, func() time.Time { return now })

	cancelled, err := lifecycle.CancelInTransaction(ctx, f.invoiceID)
	if err != nil {
		t.Fatalf("cancel invoice: %v", err)
	}
	if !cancelled || f.invoiceRepo.invoices[f.invoiceID].Status != crypto_invoice.InvoiceStatusCancelled {
		t.Fatalf("cancelled/status = %t/%s", cancelled, f.invoiceRepo.invoices[f.invoiceID].Status)
	}
	if len(f.webhooks.events) != 1 || f.webhooks.events[0].Event != WebhookInvoiceCancelled {
		t.Fatalf("events = %#v, want one invoice.cancelled", f.webhooks.events)
	}

	cancelled, err = lifecycle.CancelInTransaction(ctx, f.invoiceID)
	if err != nil || cancelled || len(f.webhooks.events) != 1 {
		t.Fatalf("idempotent cancel = (%t,%v), events=%d", cancelled, err, len(f.webhooks.events))
	}
}

func TestInvoiceLifecycle_CancelRejectsFinalInvoice(t *testing.T) {
	ctx := context.Background()
	f := setupTestFixture(t, 10_000_000)
	f.invoiceRepo.invoices[f.invoiceID].Status = crypto_invoice.InvoiceStatusCompleted
	lifecycle := NewInvoiceLifecycleService(f.invoiceRepo, f.paymentRepo, f.webhooks, &noopTxManager{}, nil)

	cancelled, err := lifecycle.CancelInTransaction(ctx, f.invoiceID)
	if cancelled || !errors.Is(err, ErrInvoiceNotCancellable) {
		t.Fatalf("cancel completed invoice = (%t,%v), want ErrInvoiceNotCancellable", cancelled, err)
	}
	if len(f.webhooks.events) != 0 {
		t.Fatalf("completed cancellation emitted events: %#v", f.webhooks.events)
	}
}

// TestPaymentCycle_ZeroThreshold_ImmediateSweep verifies the zero-threshold policy:
// threshold=0 → MarkSweepPending after confirmation (not Release).
func TestPaymentCycle_ZeroThreshold_ImmediateSweep(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 0) // zero threshold → immediate sweep

	// Transfer with enough confirmations to go Detected → Confirming → Confirmed in one event
	event := f.makeTransferEvent(5_000_000)
	event.Confirmations = 19 // already fully confirmed
	event.Finality = FinalitySolidified
	txHash := event.TxHash

	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// Verify: payment reached finality and was credited.
	payment, _ := f.paymentRepo.FindByTxHash(ctx, txHash)
	if payment.Status != crypto_payment.PaymentStatusSettled {
		t.Errorf("status = %q, want settled", payment.Status)
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
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	// Send dust amount (999 < default dust threshold 1000)
	event := f.makeTransferEvent(999)
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// Verify: no payment created
	payment, err := f.paymentRepo.FindByTxHash(ctx, event.TxHash)
	if err != nil {
		t.Fatalf("FindByTxHash: %v", err)
	}
	if payment != nil {
		t.Error("dust payment should NOT be created")
	}
}

func TestPaymentCycle_FindByTxHashErrorStopsProcessing(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)
	f.paymentRepo.findByTxHashErr = fmt.Errorf("database unavailable")

	event := f.makeTransferEvent(5_000_000)
	err := f.processor.ProcessEvent(ctx, event)
	if err == nil {
		t.Fatal("ProcessEvent should return lookup errors")
	}
	if len(f.paymentRepo.payments) != 0 {
		t.Fatalf("expected no payment creation after lookup error, got %d", len(f.paymentRepo.payments))
	}
}

func TestPaymentCycle_InvoiceTokenMustMatchBlockchainContract(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	// Empty TokenContract represents native TRX. The leased invoice expects
	// the fixture's TRC-20 token.
	nativeToken := &token.Token{}
	nativeToken.ID = id.New()
	f.processor.tokenResolver = &mockTokenRepo{tok: nativeToken}

	event := f.makeTransferEvent(5_000_000)
	event.TokenContract = ""
	err := f.processor.ProcessEvent(ctx, event)
	if err != nil {
		t.Fatalf("mismatched token must not stall the watcher: %v", err)
	}
	if len(f.paymentRepo.payments) != 0 {
		t.Fatalf("expected no payment for mismatched token, got %d", len(f.paymentRepo.payments))
	}
	inv, getErr := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if !inv.ReceivedAmount.IsZero() || inv.Status != crypto_invoice.InvoiceStatusCreated {
		t.Fatalf("mismatched token mutated invoice: status=%s received=%s", inv.Status, inv.ReceivedAmount.String())
	}
}

func TestPaymentCycle_FeeLookupFailureCreatesNoFinancialEffects(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	feeRepo := newMockFeeScheduleRepo()
	feeRepo.errors[feeRepo.key(&f.merchantID, f.tokenID, FeeDirectionProcessing)] = fmt.Errorf("fee database unavailable")
	f.processor.feeResolver = NewFeeConfigResolver(feeRepo)

	event := f.makeTransferEvent(5_000_000)
	err := f.processor.ProcessEvent(ctx, event)
	if err == nil {
		t.Fatal("fee lookup failure must stop payment processing")
	}
	if len(f.paymentRepo.payments) != 0 {
		t.Fatalf("expected no payment after fee lookup failure, got %d", len(f.paymentRepo.payments))
	}
	inv, getErr := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if !inv.ReceivedAmount.IsZero() || inv.Status != crypto_invoice.InvoiceStatusCreated {
		t.Fatalf("fee lookup failure mutated invoice: status=%s received=%s", inv.Status, inv.ReceivedAmount.String())
	}
}

// TestPaymentCycle_Idempotency verifies that processing the same tx twice doesn't duplicate.
func TestPaymentCycle_Idempotency(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
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

// TestPaymentCycle_PastExpiresAtStillMatchesOpenLease verifies wall-clock
// expiry is the lifecycle service's maintenance job: a still-Created leased invoice accepts the transfer.
func TestPaymentCycle_PastExpiresAtStillMatchesOpenLease(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	inv, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	inv.ExpiresAt = time.Now().Add(-1 * time.Hour)
	f.invoiceRepo.invoices[f.invoiceID] = inv

	event := f.makeTransferEvent(5_000_000)
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(f.paymentRepo.payments) != 1 {
		t.Fatalf("open created invoice must still match, got %d payments", len(f.paymentRepo.payments))
	}
}

func TestPaymentCycle_ExpiredStatusWithoutAllocationIsUnmatched(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	inv, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	inv.Status = crypto_invoice.InvoiceStatusExpired
	inv.ExpiresAt = time.Now().Add(-1 * time.Hour)
	f.invoiceRepo.invoices[f.invoiceID] = inv

	event := f.makeTransferEvent(5_000_000)
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(f.paymentRepo.payments) != 0 {
		t.Fatalf("expired invoice without allocation history must not match, got %d", len(f.paymentRepo.payments))
	}
}

// TestPaymentCycle_UnknownWallet verifies events for unknown addresses are silently skipped.
func TestPaymentCycle_UnknownWallet(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
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

type memAllocRepo struct {
	rows []WalletAllocation
}

func (r *memAllocRepo) Open(_ context.Context, alloc WalletAllocation) error {
	r.rows = append(r.rows, alloc)
	return nil
}

func (r *memAllocRepo) CloseOpenForWallet(_ context.Context, walletID id.ID, reason AllocationCloseReason, closedAt, cooldownUntil time.Time) (*WalletAllocation, error) {
	for i := range r.rows {
		if r.rows[i].WalletID == walletID && r.rows[i].ClosedAt == nil {
			r.rows[i].ClosedAt = &closedAt
			r.rows[i].CooldownUntil = &cooldownUntil
			r.rows[i].CloseReason = reason
			row := r.rows[i]
			return &row, nil
		}
	}
	return nil, nil
}

func (r *memAllocRepo) GetOpenByWallet(_ context.Context, walletID id.ID) (*WalletAllocation, error) {
	for i := range r.rows {
		if r.rows[i].WalletID == walletID && r.rows[i].ClosedAt == nil {
			row := r.rows[i]
			return &row, nil
		}
	}
	return nil, nil
}

func (r *memAllocRepo) FindCovering(_ context.Context, walletID id.ID, ts time.Time) ([]WalletAllocation, error) {
	var out []WalletAllocation
	for _, row := range r.rows {
		if row.WalletID == walletID && row.Covers(ts) {
			out = append(out, row)
		}
	}
	return out, nil
}

func TestPaymentCycle_LateTransferMatchesClosedAllocation(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	inv, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	inv.Status = crypto_invoice.InvoiceStatusExpired
	inv.ExpiresAt = time.Now().Add(-time.Hour)
	f.invoiceRepo.invoices[f.invoiceID] = inv

	leasedAt := time.Now().Add(-20 * time.Minute)
	closedAt := time.Now().Add(-5 * time.Minute)
	cooldownUntil := time.Now().Add(25 * time.Minute)
	f.processor.allocations = &memAllocRepo{rows: []WalletAllocation{{
		ID:            id.New(),
		WalletID:      f.walletID,
		InvoiceID:     f.invoiceID,
		MerchantID:    f.merchantID,
		TokenID:       f.tokenID,
		NetworkID:     f.networkID,
		LeasedAt:      leasedAt,
		ClosedAt:      &closedAt,
		CooldownUntil: &cooldownUntil,
		CloseReason:   AllocationCloseExpired,
	}}}

	event := f.makeTransferEvent(5_000_000)
	event.Timestamp = closedAt.Add(-time.Minute)
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(f.paymentRepo.payments) != 1 {
		t.Fatalf("late transfer in closed allocation window must create a payment, got %d", len(f.paymentRepo.payments))
	}
	inv, _ = f.invoiceRepo.GetByID(ctx, f.invoiceID)
	if inv.Status != crypto_invoice.InvoiceStatusExpired {
		t.Fatalf("GET invoice status must stay expired, got %s", inv.Status)
	}
	if inv.ReceivedAmount.String() != "5000000" {
		t.Fatalf("late funds must still be recorded, received=%s", inv.ReceivedAmount.String())
	}
}

func TestPaymentCycle_ReuseLastSecondCreditsFirstAllocation(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	inv, _ := f.invoiceRepo.GetByID(ctx, f.invoiceID)
	inv.Status = crypto_invoice.InvoiceStatusExpired
	f.invoiceRepo.invoices[f.invoiceID] = inv

	now := time.Now().UTC()
	closed := now.Add(-time.Second)
	cooldown := now
	bLeased := cooldown.Add(time.Millisecond)
	invoiceB := crypto_invoice.NewCryptoInvoice(f.merchantID, f.tokenID, types.NewRawAmountFromInt64(5_000_000))
	invoiceB.Status = crypto_invoice.InvoiceStatusCreated
	f.invoiceRepo.invoices[invoiceB.ID] = invoiceB

	f.processor.allocations = &memAllocRepo{rows: []WalletAllocation{
		{
			ID: id.New(), WalletID: f.walletID, InvoiceID: f.invoiceID,
			MerchantID: f.merchantID, TokenID: f.tokenID, NetworkID: f.networkID,
			LeasedAt: now.Add(-10 * time.Minute), ClosedAt: &closed, CooldownUntil: &cooldown,
			CloseReason: AllocationCloseExpired,
		},
		{
			ID: id.New(), WalletID: f.walletID, InvoiceID: invoiceB.ID,
			MerchantID: f.merchantID, TokenID: f.tokenID, NetworkID: f.networkID,
			LeasedAt: bLeased,
		},
	}}

	event := f.makeTransferEvent(5_000_000)
	event.Timestamp = cooldown
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(f.paymentRepo.payments) != 1 {
		t.Fatalf("expected one payment, got %d", len(f.paymentRepo.payments))
	}
	for _, payment := range f.paymentRepo.payments {
		if payment.InvoiceID != f.invoiceID {
			t.Fatalf("last second of A must credit A, got %s", payment.InvoiceID)
		}
	}
}

func TestPaymentCycle_GapAfterCooldownNeverCreditsB(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	now := time.Now().UTC()
	closed := now.Add(-2 * time.Minute)
	cooldown := now.Add(-time.Minute)
	bLeased := now
	invoiceB := crypto_invoice.NewCryptoInvoice(f.merchantID, f.tokenID, types.NewRawAmountFromInt64(5_000_000))
	invoiceB.Status = crypto_invoice.InvoiceStatusCreated
	f.invoiceRepo.invoices[invoiceB.ID] = invoiceB

	f.processor.allocations = &memAllocRepo{rows: []WalletAllocation{
		{
			ID: id.New(), WalletID: f.walletID, InvoiceID: f.invoiceID,
			MerchantID: f.merchantID, TokenID: f.tokenID, NetworkID: f.networkID,
			LeasedAt: now.Add(-10 * time.Minute), ClosedAt: &closed, CooldownUntil: &cooldown,
			CloseReason: AllocationCloseExpired,
		},
		{
			ID: id.New(), WalletID: f.walletID, InvoiceID: invoiceB.ID,
			MerchantID: f.merchantID, TokenID: f.tokenID, NetworkID: f.networkID,
			LeasedAt: bLeased,
		},
	}}

	event := f.makeTransferEvent(5_000_000)
	event.Timestamp = cooldown.Add(time.Second) // after A, before B.leased_at
	if !event.Timestamp.Before(bLeased) {
		event.Timestamp = bLeased.Add(-time.Millisecond)
	}
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(f.paymentRepo.payments) != 0 {
		t.Fatal("gap timestamp must never credit B")
	}
}

func TestPaymentCycle_TimestampBeforeOpenLeaseIsNeedsReview(t *testing.T) {
	ctx := context.Background()
	ctx = tenant.WithTxManager(ctx, &noopTxManager{})
	f := setupTestFixture(t, 10_000_000)

	now := time.Now().UTC()
	f.processor.allocations = &memAllocRepo{rows: []WalletAllocation{{
		ID: id.New(), WalletID: f.walletID, InvoiceID: f.invoiceID,
		MerchantID: f.merchantID, TokenID: f.tokenID, NetworkID: f.networkID,
		LeasedAt: now,
	}}}

	event := f.makeTransferEvent(5_000_000)
	event.Timestamp = now.Add(-time.Second)
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(f.paymentRepo.payments) != 0 {
		t.Fatal("ts before open leased_at must not auto-match current lease")
	}
}

// memPaymentReceipts accounts each chain effect once, like the receipt intake.
type memPaymentReceipts struct{ byEffect map[string]id.ID }

func (r *memPaymentReceipts) RecordPaymentReceipt(_ context.Context, p *crypto_payment.CryptoPayment, _ BlockchainEvent) (id.ID, error) {
	if r.byEffect == nil {
		r.byEffect = make(map[string]id.ID)
	}
	key := fmt.Sprintf("%s|%s|%d", p.NetworkID, p.TxHash, p.EventIndex)
	if existing, ok := r.byEffect[key]; ok {
		return existing, nil
	}
	r.byEffect[key] = id.New()
	return r.byEffect[key], nil
}
