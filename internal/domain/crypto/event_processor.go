package crypto

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/posting"
	"metapus/pkg/logger"
)

// _defaultDustThresholdMinorUnits is the minimum amount (in token minor units) to accept.
// Transactions below this are ignored as dust spam.
// Default: 1000 minor units = 0.001 USDT (6 decimals).
// Override per-processor via EventProcessorConfig.DustThreshold.
func _defaultDustThreshold() types.CryptoAmount {
	return types.NewCryptoAmountFromInt64(1000)
}

// InvoiceAccessor provides read/write access to crypto invoices.
// EventProcessor depends on this narrow interface instead of the raw Repository
// to ensure that service-layer decorators (logging, outbox, event log) are applied.
//
// Satisfied by domain.DocumentService[*crypto_invoice.CryptoInvoice]
// or by crypto_invoice.Repository (for tests).
type InvoiceAccessor interface {
	GetByID(ctx context.Context, id id.ID) (*crypto_invoice.CryptoInvoice, error)
	Update(ctx context.Context, entity *crypto_invoice.CryptoInvoice) error
	Create(ctx context.Context, entity *crypto_invoice.CryptoInvoice) error
}

// TokenResolver provides read access to the token catalog for the event processor.
type TokenResolver interface {
	FindByContractAndNetwork(ctx context.Context, contract string, networkID id.ID) (*token.Token, error)
}

// EventProcessor orchestrates the lifecycle of blockchain events:
// match wallet → find invoice → create/update payment → FSM transition → post.
//
// This is the central business logic component — chain-agnostic.
// Chain watchers (TRON, ETH, etc.) feed normalized BlockchainEvent into this processor.
type EventProcessor struct {
	fsm            *PaymentFSM
	walletSvc      *wallet.Service
	invoiceSvc     InvoiceAccessor
	paymentRepo    crypto_payment.Repository
	postingEngine  *posting.Engine
	txManager      tx.Manager
	numerator      numerator.Generator
	dustThreshold  types.CryptoAmount
	sweepResolver  *SweepConfigResolver
	feeResolver    *FeeConfigResolver   // optional: if nil, fee = 0
	tokenResolver  TokenResolver        // required for persistent wallet top-ups
	nowFunc        func() time.Time     // clock source, default time.Now().UTC()
}

// Option configures EventProcessor. Use with NewEventProcessor.
type Option func(*EventProcessor)

// WithClock overrides the time source (for testing).
// In production, leave unset — defaults to time.Now().UTC().
func WithClock(fn func() time.Time) Option {
	return func(p *EventProcessor) { p.nowFunc = fn }
}

// EventProcessorConfig holds dependencies for the event processor.
type EventProcessorConfig struct {
	FSM              *PaymentFSM
	WalletSvc        *wallet.Service
	InvoiceSvc       InvoiceAccessor
	PaymentRepo      crypto_payment.Repository
	PostingEngine    *posting.Engine
	TxManager        tx.Manager
	Numerator        numerator.Generator
	SweepResolver    *SweepConfigResolver
	FeeResolver      *FeeConfigResolver // optional: nil → fee = 0
	TokenResolver    TokenResolver
	// DustThreshold is the minimum amount to accept. Zero = use default (1000 minor units).
	DustThreshold types.CryptoAmount
}

// NewEventProcessor creates a new event processor.
func NewEventProcessor(cfg EventProcessorConfig, opts ...Option) *EventProcessor {
	threshold := cfg.DustThreshold
	if threshold.IsZero() {
		threshold = _defaultDustThreshold()
	}

	p := &EventProcessor{
		fsm:              cfg.FSM,
		walletSvc:        cfg.WalletSvc,
		invoiceSvc:       cfg.InvoiceSvc,
		paymentRepo:      cfg.PaymentRepo,
		postingEngine:    cfg.PostingEngine,
		txManager:        cfg.TxManager,
		numerator:        cfg.Numerator,
		dustThreshold:    threshold,
		sweepResolver:    cfg.SweepResolver,
		feeResolver:      cfg.FeeResolver,
		tokenResolver:    cfg.TokenResolver,
		nowFunc:          func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ProcessEvent handles a single blockchain event end-to-end.
// This is the main entry point called by chain watchers.
//
// Algorithm:
//  1. Match: event.ToAddress → find Wallet → find active CryptoInvoice
//  2. Idempotency: check CryptoPayment.TxHash — if exists, update confirmations
//  3. Create Payment: if new tx → create CryptoPayment(Detected)
//  4. FSM Transition: Detected→Confirming (confs ≥ 1), Confirming→Confirmed (confs ≥ required)
//  5. Invoice Update: update ReceivedAmount, recalculate Status
//  6. Post: on Confirmed → posting engine records register movements
//  7. Wallet: on Confirmed → mark wallet as SweepPending
func (p *EventProcessor) ProcessEvent(ctx context.Context, event BlockchainEvent) error {
	return p.txManager.RunInTransaction(ctx, func(ctx context.Context) error {
		return p.processEventInTx(ctx, event)
	})
}

func (p *EventProcessor) processEventInTx(ctx context.Context, event BlockchainEvent) error {
	// Guard: reject non-positive amounts (dust attacks, contract bugs, zero-value events).
	// Reorg events bypass this check — they need processing regardless of amount.
	if event.EventType != EventTypeReorg && !event.Amount.IsPositive() {
		logger.Warn(ctx, "ignoring non-positive amount event",
			"tx_hash", event.TxHash,
			"amount", event.Amount.String(),
			"network_id", event.NetworkID,
		)
		return nil
	}

	// Guard: reject dust amounts below threshold.
	// Prevents spam attacks where attacker sends thousands of micro-transactions
	// (e.g., 0.000001 USDT) to create CryptoPayment records and overload the DB.
	if event.EventType == EventTypeTransfer && event.Amount.Cmp(p.dustThreshold) < 0 {
		logger.Debug(ctx, "ignoring dust amount event",
			"tx_hash", event.TxHash,
			"amount", event.Amount.String(),
			"threshold", p.dustThreshold.String(),
		)
		return nil
	}

	// Step 1: Idempotency check — do we already have a payment for this tx?
	// This MUST come before wallet lookup so that confirmation re-polls
	// (which may not carry ToAddress) can update existing payments.
	existing, err := p.paymentRepo.FindByTxHash(ctx, event.TxHash)
	if err == nil && existing != nil {
		// Lock the payment row to serialize concurrent confirmation updates.
		// Without this, consumer + confirmation loop can both read status=confirming
		// and race into Post(), causing duplicate register movements.
		// Single-row FOR UPDATE on PK — safe from deadlocks.
		existing, err = p.paymentRepo.GetByIDForUpdate(ctx, existing.ID)
		if err != nil {
			return fmt.Errorf("lock payment %s for update: %w", existing.ID, err)
		}
		return p.handleConfirmationUpdate(ctx, existing, event)
	}

	// Step 2: Match wallet (only needed for new payments)
	w, err := p.walletSvc.FindByAddress(ctx, event.NetworkID, event.ToAddress)
	if err != nil {
		// Not our wallet — skip silently (normal for chain watchers monitoring many addresses)
		logger.Debug(ctx, "ignoring event for unknown wallet",
			"address", event.ToAddress,
			"tx_hash", event.TxHash,
		)
		return nil
	}

	// Step 3: Reorg handling
	if event.EventType == EventTypeReorg {
		logger.Warn(ctx, "reorg event for unknown payment",
			"tx_hash", event.TxHash,
		)
		return nil
	}

	// Step 4: Find the active invoice for this wallet
	invoice, err := p.findActiveInvoice(ctx, w)
	if err != nil {
		// If wallet is persistent, we generate a Top-Up invoice on the fly
		if w.IsPersistent() {
			invoice, err = p.createTopUpInvoice(ctx, w, event)
			if err != nil {
				return fmt.Errorf("create top-up invoice: %w", err)
			}
		} else {
			logger.Warn(ctx, "no active invoice for wallet",
				"wallet_id", w.ID,
				"address", w.Address,
				"tx_hash", event.TxHash,
			)
			return nil //nolint:nilerr // No active invoice — funds will be reconciled later
		}
	}

	// Step 5: Create new CryptoPayment
	payment := crypto_payment.NewCryptoPayment(
		invoice.ID,
		invoice.MerchantID,
		invoice.TokenID,
		w.ID,
		event.TxHash,
		event.FromAddress,
		event.Amount,
		event.BlockNumber,
		event.RequiredConfs,
	)
	payment.Date = event.Timestamp
	payment.Confirmations = event.Confirmations

	// Snapshot fee config onto the payment.
	// Fail-soft: if lookup fails, fee = 0 (no fee charged).
	if p.feeResolver != nil {
		fee, err := p.feeResolver.Resolve(ctx, invoice.MerchantID, invoice.TokenID, FeeDirectionProcessing)
		if err != nil {
			logger.Warn(ctx, "fee schedule lookup failed, using zero fee",
				"merchant_id", invoice.MerchantID,
				"token_id", invoice.TokenID,
				"error", err,
			)
			// Mark payment for ops alerting: fee was not resolved, zero fee applied.
			// This is a fail-soft approach: better to process a payment with 0 fee
			// than to lose the payment entirely. The attribute enables reconciliation.
			payment.SetAttribute("_fee_unresolved", true)
		} else {
			payment.SetFeeConfig(fee.FixedFee, fee.PercentBP, fee.MinFee, fee.MaxFee)
		}
	}

	// Generate sequential number via the system numerator (e.g. CP-2026-00001).
	// Falls back to UUID-based number only if numerator is unavailable.
	if p.numerator != nil {
		cfg := numerator.DefaultConfig("CP")
		number, err := p.numerator.GetNextNumber(ctx, cfg, &numerator.Options{Strategy: numerator.StrategyStrict}, event.Timestamp)
		if err != nil {
			logger.Warn(ctx, "numerator failed for crypto payment, using fallback",
				"error", err,
				"tx_hash", event.TxHash,
			)
			payment.Number = fmt.Sprintf("CP-%s", id.New().String()[:8])
		} else {
			payment.Number = number
		}
	} else {
		payment.Number = fmt.Sprintf("CP-%s", id.New().String()[:8])
	}

	if err := p.paymentRepo.Create(ctx, payment); err != nil {
		return fmt.Errorf("create payment for tx %s: %w", event.TxHash, err)
	}

	logger.Info(ctx, "payment created",
		"payment_id", payment.ID,
		"invoice_id", invoice.ID,
		"tx_hash", event.TxHash,
		"amount", event.Amount.String(),
	)

	// Step 6: Update invoice received amount
	if err := p.updateInvoiceAmount(ctx, invoice, event); err != nil {
		return fmt.Errorf("update invoice amount: %w", err)
	}

	// Step 7: Process confirmations for the new payment
	return p.processConfirmations(ctx, payment, event)
}

// handleConfirmationUpdate processes confirmation updates for existing payments.
func (p *EventProcessor) handleConfirmationUpdate(
	ctx context.Context,
	payment *crypto_payment.CryptoPayment,
	event BlockchainEvent,
) error {
	// Handle reorg
	if event.EventType == EventTypeReorg {
		return p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusReorged,
			"chain_reorg", TransitionMetadata{
				BlockNumber: event.BlockNumber,
				TxHash:      event.TxHash,
			})
	}

	// Update confirmation count
	if event.Confirmations > payment.Confirmations {
		payment.Confirmations = event.Confirmations
		if err := p.paymentRepo.Update(ctx, payment); err != nil {
			return fmt.Errorf("update confirmations: %w", err)
		}
	}

	return p.processConfirmations(ctx, payment, event)
}

// processConfirmations evaluates whether FSM transitions should occur based on confirmations.
//
// Uses sequential if-checks instead of switch+fallthrough to handle the case
// where a payment goes Detected → Confirming → Confirmed in a single event
// (e.g., watcher first sees a tx with 20+ confirmations already).
//
// All errors are propagated — if any step fails, the entire transaction rolls back
// and the confirmation poll loop retries on the next tick (10s). This prevents:
//   - Wallet leak: stuck in 'leased' if MarkSweepPending/Release fails
//   - Invoice desync: stuck in 'paid' if confirmInvoice fails
func (p *EventProcessor) processConfirmations(
	ctx context.Context,
	payment *crypto_payment.CryptoPayment,
	event BlockchainEvent,
) error {
	// Step 1: Detected → Confirming (first confirmation)
	if payment.Status == crypto_payment.PaymentStatusDetected && event.Confirmations >= 1 {
		if err := p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusConfirming,
			"first_confirmation", TransitionMetadata{
				Confirmations: event.Confirmations,
				BlockNumber:   event.BlockNumber,
			}); err != nil {
			return fmt.Errorf("transition to confirming: %w", err)
		}
	}

	// Step 2: Confirming → Confirmed (reached required confirmations)
	if payment.Status == crypto_payment.PaymentStatusConfirming && event.Confirmations >= payment.RequiredConfs {
		// Use postingEngine to generate and record register movements.
		// The engine internally sets payment.Posted = true and increments PostedVersion.
		// We pass fsm.Transition as the update callback to eliminate double-writes:
		// the FSM transition will persist the new Status and the updated Posted fields in one UPDATE.
		if err := p.postingEngine.Post(ctx, payment, func(ctx context.Context) error {
			return p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusConfirmed,
				"confirmed", TransitionMetadata{
					Confirmations: event.Confirmations,
					RequiredConfs: payment.RequiredConfs,
				})
		}); err != nil {
			return fmt.Errorf("post payment: %w", err)
		}

		// Confirm invoice — transitions paid → confirmed.
		// Returns true if invoice was fully paid and confirmed.
		invoiceConfirmed, err := p.confirmInvoice(ctx, payment.InvoiceID)
		if err != nil {
			return fmt.Errorf("confirm invoice %s: %w", payment.InvoiceID, err)
		}

		// Handle wallet status ONLY when invoice is fully confirmed.
		// If invoice is partially_paid, wallet must stay leased so subsequent
		// payments match the same invoice via findActiveInvoice.
		if invoiceConfirmed {
			if err := p.handleWalletAfterConfirm(ctx, payment); err != nil {
				return fmt.Errorf("handle wallet after confirm: %w", err)
			}
		}
	}

	return nil
}

// updateInvoiceAmount updates the invoice's received amount, status, and overpaid amount.
//
// Three-way classification:
//   - received < expected → partially_paid
//   - received == expected → paid
//   - received > expected → overpaid (if excess >= tolerance) or paid (if dust excess)
func (p *EventProcessor) updateInvoiceAmount(ctx context.Context, invoice *crypto_invoice.CryptoInvoice, event BlockchainEvent) error {
	// Add received amount
	invoice.ReceivedAmount = invoice.ReceivedAmount.Add(event.Amount)

	// Calculate excess
	excess := invoice.ReceivedAmount.Sub(invoice.ExpectedAmount)

	switch {
	case excess.IsPositive():
		// Received > expected — check tolerance before marking overpaid
		invoice.OverpaidAmount = excess

		if p.isOverpaymentSignificant(ctx, invoice.MerchantID, invoice.TokenID, excess) {
			invoice.Status = crypto_invoice.InvoiceStatusOverpaid
			logger.Warn(ctx, "invoice overpaid",
				"invoice_id", invoice.ID,
				"expected", invoice.ExpectedAmount.String(),
				"received", invoice.ReceivedAmount.String(),
				"overpaid", excess.String(),
			)
		} else {
			// Dust overpayment — treat as exact payment
			invoice.Status = crypto_invoice.InvoiceStatusPaid
		}
	case excess.IsZero():
		// Exact payment
		invoice.Status = crypto_invoice.InvoiceStatusPaid
		invoice.OverpaidAmount = types.ZeroCryptoAmount()
	default:
		// Partial payment
		if invoice.ReceivedAmount.IsPositive() {
			invoice.Status = crypto_invoice.InvoiceStatusPartiallyPaid
		}
		invoice.OverpaidAmount = types.ZeroCryptoAmount()
	}

	if err := p.invoiceSvc.Update(ctx, invoice); err != nil {
		return fmt.Errorf("update invoice %s: %w", invoice.ID, err)
	}

	return nil
}

// isOverpaymentSignificant checks if the overpayment exceeds the configured tolerance.
// Falls back to "any overpayment is significant" if config resolution fails.
func (p *EventProcessor) isOverpaymentSignificant(ctx context.Context, merchantID, tokenID id.ID, excess types.CryptoAmount) bool {
	if p.sweepResolver == nil {
		return excess.IsPositive()
	}

	cfg, err := p.sweepResolver.Resolve(ctx, merchantID, tokenID)
	if err != nil {
		logger.Warn(ctx, "overpayment tolerance resolve failed, treating as significant",
			"merchant_id", merchantID,
			"token_id", tokenID,
			"error", err,
		)
		return excess.IsPositive()
	}

	return cfg.IsOverpaymentSignificant(excess)
}

// confirmInvoice updates the invoice status to Confirmed if it is fully paid or overpaid.
// Returns true if the invoice was confirmed (transitioned from paid/overpaid → confirmed).
// OverpaidAmount is preserved after confirmation as a marker for refund workflows.
func (p *EventProcessor) confirmInvoice(ctx context.Context, invoiceID id.ID) (bool, error) {
	invoice, err := p.invoiceSvc.GetByID(ctx, invoiceID)
	if err != nil {
		return false, fmt.Errorf("get invoice %s: %w", invoiceID, err)
	}

	// Confirm both exact-paid and overpaid invoices.
	// OverpaidAmount persists as a marker — it's NOT reset on confirmation.
	if invoice.Status == crypto_invoice.InvoiceStatusPaid ||
		invoice.Status == crypto_invoice.InvoiceStatusOverpaid {
		invoice.Status = crypto_invoice.InvoiceStatusConfirmed
		if err := p.invoiceSvc.Update(ctx, invoice); err != nil {
			return false, fmt.Errorf("confirm invoice %s: %w", invoiceID, err)
		}
		return true, nil
	}

	return false, nil
}

// createTopUpInvoice generates a system CryptoInvoice for an incoming payment to a persistent wallet.
func (p *EventProcessor) createTopUpInvoice(ctx context.Context, w *wallet.Wallet, event BlockchainEvent) (*crypto_invoice.CryptoInvoice, error) {
	if p.tokenResolver == nil {
		return nil, fmt.Errorf("tokenResolver is not configured (required for top-ups)")
	}

	tok, err := p.tokenResolver.FindByContractAndNetwork(ctx, event.TokenContract, w.NetworkID)
	if err != nil {
		return nil, fmt.Errorf("resolve token %s on network %s: %w", event.TokenContract, w.NetworkID, err)
	}

	invoice := crypto_invoice.NewCryptoInvoice(
		*w.MerchantID,
		tok.ID,
		event.Amount,
	)

	invoice.WalletID = &w.ID
	invoice.ExternalID = fmt.Sprintf("topup_%s", event.TxHash)
	invoice.CustomerEmail = w.CustomerRef
	invoice.Status = crypto_invoice.InvoiceStatusPaid // Start as Paid so it can be Confirmed later

	// Assign a numerator if available
	if p.numerator != nil {
		cfg := numerator.DefaultConfig("CI")
		number, err := p.numerator.GetNextNumber(ctx, cfg, &numerator.Options{Strategy: numerator.StrategyStrict}, p.nowFunc())
		if err == nil {
			invoice.Number = number
		} else {
			invoice.Number = fmt.Sprintf("CI-%s", id.New().String()[:8])
		}
	} else {
		invoice.Number = fmt.Sprintf("CI-%s", id.New().String()[:8])
	}

	if err := p.invoiceSvc.Create(ctx, invoice); err != nil {
		return nil, fmt.Errorf("save top-up invoice: %w", err)
	}

	logger.Info(ctx, "created top-up invoice for persistent wallet",
		"invoice_id", invoice.ID,
		"wallet_id", w.ID,
		"customer_ref", w.CustomerRef,
		"amount", event.Amount.String(),
	)

	return invoice, nil
}

// findActiveInvoice finds the active (leased) invoice for a wallet.
func (p *EventProcessor) findActiveInvoice(ctx context.Context, w *wallet.Wallet) (*crypto_invoice.CryptoInvoice, error) {
	if w.LeasedForID == nil {
		return nil, fmt.Errorf("wallet %s is not leased", w.ID)
	}

	invoice, err := p.invoiceSvc.GetByID(ctx, *w.LeasedForID)
	if err != nil {
		return nil, fmt.Errorf("get invoice %s: %w", *w.LeasedForID, err)
	}

	// Only accept payments for active invoices
	if invoice.Status == crypto_invoice.InvoiceStatusExpired ||
		invoice.Status == crypto_invoice.InvoiceStatusCancelled {
		return nil, fmt.Errorf("invoice %s is %s (not active)", invoice.ID, invoice.Status)
	}

	// Check expiration
	if p.nowFunc().After(invoice.ExpiresAt) && invoice.Status == crypto_invoice.InvoiceStatusCreated {
		invoice.Status = crypto_invoice.InvoiceStatusExpired
		if err := p.invoiceSvc.Update(ctx, invoice); err != nil {
			return nil, fmt.Errorf("expire invoice %s: %w", invoice.ID, err)
		}
		return nil, fmt.Errorf("invoice %s has expired", invoice.ID)
	}

	return invoice, nil
}

// handleWalletAfterConfirm decides wallet status after a payment is confirmed.
//
// Logic:
//   - threshold=0 → sweep immediately (legacy behavior, backward compatible)
//   - threshold>0, transient wallet → release to free (available for new invoices);
//     sweep evaluation job will handle sweep_pending when balance reaches threshold
//   - persistent wallet → never released (stays assigned to customer)
func (p *EventProcessor) handleWalletAfterConfirm(ctx context.Context, payment *crypto_payment.CryptoPayment) error {
	// If no sweep resolver is configured, fall back to legacy behavior
	if p.sweepResolver == nil {
		return p.walletSvc.MarkSweepPending(ctx, payment.WalletID)
	}

	sweepCfg, err := p.sweepResolver.Resolve(ctx, payment.MerchantID, payment.TokenID)
	if err != nil {
		// On error, fall back to legacy behavior
		logger.Warn(ctx, "sweep config resolve failed, using legacy sweep",
			"merchant_id", payment.MerchantID,
			"token_id", payment.TokenID,
			"error", err,
		)
		return p.walletSvc.MarkSweepPending(ctx, payment.WalletID)
	}

	if sweepCfg.IsZeroThreshold() {
		// Legacy behavior: sweep after every payment
		return p.walletSvc.MarkSweepPending(ctx, payment.WalletID)
	}

	// Threshold mode: check wallet allocation
	w, err := p.walletSvc.GetByID(ctx, payment.WalletID)
	if err != nil {
		return fmt.Errorf("get wallet %s: %w", payment.WalletID, err)
	}

	if w.IsTransient() {
		// Release transient wallet → free (available for new invoices)
		// Sweep evaluation job will check accumulated balance periodically
		return p.walletSvc.ReleaseWallet(ctx, payment.WalletID)
	}

	// Persistent wallets: no status change (stays 'assigned')
	// Sweep evaluation job handles them the same way
	logger.Debug(ctx, "persistent wallet stays assigned after confirm",
		"wallet_id", w.ID,
		"customer_ref", w.CustomerRef,
	)

	return nil
}
