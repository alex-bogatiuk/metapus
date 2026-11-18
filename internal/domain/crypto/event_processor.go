package crypto

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"metapus/internal/core/apperror"
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

// The threshold is denominated in microtokens (10^-6 token), independent of
// each asset's raw-unit precision.
func _defaultDustThreshold() types.RawAmount {
	return types.NewRawAmountFromInt64(1000)
}

// dustThresholdForToken converts a microtoken threshold to raw units, rounding
// up so a positive threshold never becomes zero for a low-precision asset.
func (p *EventProcessor) dustThresholdForToken(tok *token.Token) types.RawAmount {
	if tok == nil {
		return p.dustThreshold
	}
	value := p.dustThreshold.BigInt()
	if tok.DecimalPlaces >= 6 {
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(tok.DecimalPlaces-6)), nil)
		value.Mul(value, factor)
	} else {
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(6-tok.DecimalPlaces)), nil)
		value.Add(value, new(big.Int).Sub(factor, big.NewInt(1)))
		value.Quo(value, factor)
	}
	amount, _ := types.NewRawAmountFromBigInt(value)
	return amount
}

// InvoiceAccessor provides read/write access to crypto invoices.
// EventProcessor depends on this narrow interface instead of the raw Repository
// to ensure that service-layer decorators (logging, outbox, event log) are applied.
//
// Satisfied by domain.DocumentService[*crypto_invoice.CryptoInvoice]
// or by crypto_invoice.Repository (for tests).
type InvoiceAccessor interface {
	GetByID(ctx context.Context, id id.ID) (*crypto_invoice.CryptoInvoice, error)
	GetByIDForUpdate(ctx context.Context, id id.ID) (*crypto_invoice.CryptoInvoice, error)
	Update(ctx context.Context, entity *crypto_invoice.CryptoInvoice) error
	Create(ctx context.Context, entity *crypto_invoice.CryptoInvoice) error
}

// TokenResolver provides read access to the token catalog for the event processor.
type TokenResolver interface {
	FindByContractAndNetwork(ctx context.Context, contract string, networkID id.ID) (*token.Token, error)
}

// ExternalOutflowRecorder records managed-wallet outgoing transfers that were
// not created by Metapus outbound documents.
type ExternalOutflowRecorder interface {
	RecordExternalOutflow(ctx context.Context, event BlockchainEvent) error
}

// ExpectedTransferRouter hands a known signed transfer to its original
// settlement owner before external matching or direction-based early exits.
// A handled observation grants no permission to post money or sign again.
type ExpectedTransferRouter interface {
	RouteExpectedTransfer(context.Context, BlockchainEvent) (bool, error)
}

// FinancialReorgRecorder durably opens a security/finance incident and places
// the network on an outbound/allocation hold. It is invoked only when an
// already-posted payment becomes non-canonical; ledger reversal remains a
// deliberate finance operation.
type FinancialReorgRecorder interface {
	RecordFinancialReorg(ctx context.Context, payment *crypto_payment.CryptoPayment, event BlockchainEvent) error
}

// PaymentReceiptRecorder accounts the proven final transfer of a payment as a
// custody receipt and returns it. Replays of the same chain effect return the
// same receipt, so a re-included payment attributes its original receipt.
type PaymentReceiptRecorder interface {
	RecordPaymentReceipt(ctx context.Context, payment *crypto_payment.CryptoPayment, event BlockchainEvent) (id.ID, error)
}

// EventProcessor orchestrates the lifecycle of blockchain events:
// match wallet → find invoice → create/update payment → FSM transition → post.
//
// This is the central business logic component — chain-agnostic.
// Chain watchers (TRON, ETH, etc.) feed normalized BlockchainEvent into this processor.
type EventProcessor struct {
	fsm               *PaymentFSM
	walletSvc         *wallet.Service
	invoiceSvc        InvoiceAccessor
	paymentRepo       crypto_payment.Repository
	postingEngine     *posting.Engine
	txManager         tx.Manager
	numerator         numerator.Generator
	dustThreshold     types.RawAmount
	sweepResolver     *SweepConfigResolver
	feeResolver       *FeeConfigResolver // optional: nil means an explicitly free product
	tokenResolver     TokenResolver      // required for every new incoming transfer
	outflowRecorder   ExternalOutflowRecorder
	expectedTransfers ExpectedTransferRouter
	financialReorgs   FinancialReorgRecorder
	financialGuard    FinancialEventGuard
	receipts          PaymentReceiptRecorder
	webhookPublisher  LifecycleWebhookPublisher
	leaseCloser       LeaseCloser
	allocations       WalletAllocationRepository
	inbox             ChainEventInbox
	onLatePayment     func(ctx context.Context)
	nowFunc           func() time.Time // clock source, default time.Now().UTC()
	invoiceLifecycle  *InvoiceLifecycleService
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
	FSM               *PaymentFSM
	WalletSvc         *wallet.Service
	InvoiceSvc        InvoiceAccessor
	PaymentRepo       crypto_payment.Repository
	PostingEngine     *posting.Engine
	TxManager         tx.Manager
	Numerator         numerator.Generator
	SweepResolver     *SweepConfigResolver
	FeeResolver       *FeeConfigResolver // optional: nil → explicitly free product
	TokenResolver     TokenResolver
	OutflowRecorder   ExternalOutflowRecorder
	ExpectedTransfers ExpectedTransferRouter
	FinancialReorgs   FinancialReorgRecorder
	FinancialGuard    FinancialEventGuard
	Receipts          PaymentReceiptRecorder
	WebhookPublisher  LifecycleWebhookPublisher
	LeaseCloser       LeaseCloser
	Allocations       WalletAllocationRepository
	Inbox             ChainEventInbox
	OnLatePayment     func(ctx context.Context)
	// DustThreshold is measured in microtokens (10^-6 token) and scaled to the
	// asset's decimal places. Zero selects the default 1000 microtokens.
	DustThreshold types.RawAmount
}

// NewEventProcessor creates a new event processor.
func NewEventProcessor(cfg EventProcessorConfig, opts ...Option) *EventProcessor {
	threshold := cfg.DustThreshold
	if threshold.IsZero() {
		threshold = _defaultDustThreshold()
	}

	p := &EventProcessor{
		fsm:               cfg.FSM,
		walletSvc:         cfg.WalletSvc,
		invoiceSvc:        cfg.InvoiceSvc,
		paymentRepo:       cfg.PaymentRepo,
		postingEngine:     cfg.PostingEngine,
		txManager:         cfg.TxManager,
		numerator:         cfg.Numerator,
		dustThreshold:     threshold,
		sweepResolver:     cfg.SweepResolver,
		feeResolver:       cfg.FeeResolver,
		tokenResolver:     cfg.TokenResolver,
		outflowRecorder:   cfg.OutflowRecorder,
		expectedTransfers: cfg.ExpectedTransfers,
		financialReorgs:   cfg.FinancialReorgs,
		financialGuard:    cfg.FinancialGuard,
		receipts:          cfg.Receipts,
		webhookPublisher:  cfg.WebhookPublisher,
		leaseCloser:       cfg.LeaseCloser,
		allocations:       cfg.Allocations,
		inbox:             cfg.Inbox,
		onLatePayment:     cfg.OnLatePayment,
		nowFunc:           func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(p)
	}
	p.invoiceLifecycle = NewInvoiceLifecycleService(p.invoiceSvc, p.paymentRepo, p.webhookPublisher, p.txManager, p.nowFunc)
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
	if event.ObservationMode == "shadow" {
		return nil
	}
	return p.txManager.RunInTransaction(ctx, func(ctx context.Context) error {
		return p.processEventInTx(ctx, event)
	})
}

func (p *EventProcessor) processEventInTx(ctx context.Context, event BlockchainEvent) error {
	if p.expectedTransfers != nil {
		handled, err := p.expectedTransfers.RouteExpectedTransfer(ctx, event)
		if err != nil || handled {
			return err
		}
	}
	switch event.Direction {
	case TransferDirectionOutgoing:
		if p.outflowRecorder != nil {
			return p.outflowRecorder.RecordExternalOutflow(ctx, event)
		}
		logger.Warn(ctx, "outgoing managed wallet transfer observed without outflow recorder",
			"tx_hash", event.TxHash,
			"from", event.FromAddress,
			"to", event.ToAddress,
		)
		return nil
	case TransferDirectionInternal:
		logger.Info(ctx, "internal managed wallet transfer observed; skipping payment matching",
			"tx_hash", event.TxHash,
			"from", event.FromAddress,
			"to", event.ToAddress,
		)
		return nil
	}

	if locker, ok := p.financialGuard.(IncomingFinancialLocker); ok {
		if err := locker.LockIncomingEvent(ctx, event); err != nil {
			return err
		}
	}
	eventKind := event.EffectiveEventKind()
	existing, err := p.paymentRepo.FindByChainEvent(ctx, event.NetworkID, event.TxHash, string(eventKind), event.EventIndex)
	if err != nil {
		return fmt.Errorf("find payment by tx hash %s: %w", event.TxHash, err)
	}
	if existing != nil {
		if _, err := p.invoiceSvc.GetByIDForUpdate(ctx, existing.InvoiceID); err != nil {
			return fmt.Errorf("lock invoice %s before payment %s: %w", existing.InvoiceID, existing.ID, err)
		}
		paymentID := existing.ID
		existing, err = p.paymentRepo.GetByIDForUpdate(ctx, paymentID)
		if err != nil {
			return fmt.Errorf("lock payment %s for update: %w", paymentID, err)
		}
		return p.handleConfirmationUpdate(ctx, existing, event)
	}

	if event.EventType == EventTypeReorg {
		if updater, ok := p.inbox.(interface {
			UpdateReviewChainState(context.Context, BlockchainEvent) error
		}); ok {
			if err := updater.UpdateReviewChainState(ctx, event); err != nil {
				return err
			}
		}
		logger.Warn(ctx, "reorg event for unknown payment",
			"tx_hash", event.TxHash,
		)
		return nil
	}
	if event.EventType == EventTypeConfirmation {
		if updater, ok := p.inbox.(interface {
			UpdateReviewChainState(context.Context, BlockchainEvent) error
		}); ok {
			return updater.UpdateReviewChainState(ctx, event)
		}
		return nil
	}
	if event.ToAddress == "" {
		return nil
	}

	w, err := p.walletSvc.FindByAddressForUpdate(ctx, event.NetworkID, event.ToAddress)
	if err != nil {
		if !apperror.IsNotFound(err) {
			return fmt.Errorf("find wallet %s: %w", event.ToAddress, err)
		}
		logger.Debug(ctx, "ignoring event for unknown wallet",
			"address", event.ToAddress,
			"tx_hash", event.TxHash,
		)
		if _, inboxErr := p.persistTransferInbox(ctx, event, nil, nil); inboxErr != nil {
			return inboxErr
		}
		return p.markInboxTerminal(ctx, event, InboxIgnored, MatchReasonUnknownWallet, nil)
	}
	walletID := w.ID

	if p.tokenResolver == nil {
		return fmt.Errorf("token resolver is not configured")
	}
	eventToken, tokenErr := p.tokenResolver.FindByContractAndNetwork(ctx, event.TokenContract, event.NetworkID)
	if tokenErr != nil && !apperror.IsNotFound(tokenErr) {
		return fmt.Errorf("resolve event token contract %q on network %s: %w", event.TokenContract, event.NetworkID, tokenErr)
	}
	var tokenID *id.ID
	if eventToken != nil {
		tokenID = &eventToken.ID
	}

	inboxRow, err := p.persistTransferInbox(ctx, event, &walletID, tokenID)
	if err != nil {
		return err
	}
	if inboxRow != nil && inboxRow.ReceiptID != nil {
		// The final transfer is already accounted as a custody receipt; any
		// further economic decision is a separate attribution, not a payment.
		return nil
	}

	if event.EventType != EventTypeReorg && !event.Amount.IsPositive() {
		return p.markInboxRowTerminal(ctx, inboxRow, InboxUnmatched, MatchReasonNonPositive, nil)
	}
	if w.DepositRetiredAt != nil {
		if p.inbox == nil || inboxRow == nil {
			return fmt.Errorf("retired address intake requires a durable review inbox")
		}
		details := map[string]any{"customerId": w.CustomerID, "walletId": w.ID.String(), "retiredAt": w.DepositRetiredAt.UTC()}
		if w.MerchantID != nil {
			details["merchantId"] = w.MerchantID.String()
		}
		return p.markInboxRowTerminal(ctx, inboxRow, InboxNeedsReview, MatchReasonRetiredDepositAddress, details)
	}
	if apperror.IsNotFound(tokenErr) || eventToken == nil {
		return p.markInboxRowTerminal(ctx, inboxRow, InboxUnmatched, MatchReasonUnknownToken, nil)
	}
	dustLimit := p.dustThresholdForToken(eventToken)
	if event.EventType == EventTypeTransfer && event.Amount.Cmp(dustLimit) < 0 {
		return p.markInboxRowTerminal(ctx, inboxRow, InboxUnmatched, MatchReasonDust, nil)
	}

	invoice, alloc, err := p.matchInvoiceForTransfer(ctx, w, event, eventToken)
	if err != nil {
		return err
	}
	if invoice == nil {
		return nil
	}
	if p.invoiceSvc != nil {
		locked, lockErr := p.invoiceSvc.GetByIDForUpdate(ctx, invoice.ID)
		if lockErr != nil {
			return fmt.Errorf("lock invoice %s: %w", invoice.ID, lockErr)
		}
		invoice = locked
	}
	if event.SourceNonce != nil && event.SourceAddress != "" {
		replaced, replaceErr := p.paymentRepo.FindBySenderNonce(ctx, event.NetworkID, event.SourceAddress, *event.SourceNonce)
		if replaceErr != nil {
			return fmt.Errorf("find proven EVM replacement: %w", replaceErr)
		}
		if replaced != nil && replaced.TxHash != event.TxHash {
			if err := p.handleProvenReplacement(ctx, replaced, event); err != nil {
				return err
			}
			if replaced.InvoiceID == invoice.ID {
				invoice, err = p.invoiceSvc.GetByIDForUpdate(ctx, invoice.ID)
				if err != nil {
					return fmt.Errorf("reload invoice after payment replacement: %w", err)
				}
			}
		}
	}
	if invoice.SinglePayment && invoice.SelectedPaymentID != nil {
		if p.inbox == nil || inboxRow == nil {
			return fmt.Errorf("single-payment review inbox is not configured")
		}
		var allocationID id.ID
		if alloc != nil {
			allocationID = alloc.ID
		}
		return p.inbox.MarkSinglePaymentReview(ctx, inboxRow.ID, invoice.ID, allocationID)
	}

	// Step 5: Create new CryptoPayment
	payment := crypto_payment.NewCryptoPayment(
		invoice.ID,
		invoice.MerchantID,
		invoice.TokenID,
		w.ID,
		event.NetworkID,
		event.TxHash,
		event.EventIndex,
		string(eventKind),
		event.BlockHash,
		event.FromAddress,
		event.Amount,
		event.BlockNumber,
		event.RequiredConfs,
	)
	payment.Date = event.Timestamp
	payment.SourceAddress = event.SourceAddress
	payment.SourceNonce = event.SourceNonce
	payment.Confirmations = event.Confirmations
	payment.FinalityStatus = string(event.Finality)
	if event.Canonicality == CanonicalityCanonical || event.Canonicality == CanonicalityOrphaned {
		payment.Canonicality = string(event.Canonicality)
	}

	// Snapshot fee config onto the payment. Repository failures are fail-closed:
	// the surrounding transaction rolls back and the chain event is retried.
	if p.feeResolver != nil {
		fee, err := p.feeResolver.Resolve(ctx, invoice.MerchantID, invoice.TokenID, FeeDirectionProcessing)
		if err != nil {
			return fmt.Errorf("resolve processing fee for merchant %s token %s: %w", invoice.MerchantID, invoice.TokenID, err)
		}
		payment.SetFeeConfig(fee.FixedFee, fee.PercentBP, fee.MinFee, fee.MaxFee)
	}

	// Generate sequential number via the system numerator (e.g. CP-2026-00001).
	// Falls back to UUID-based number only if numerator is unavailable.
	if p.numerator != nil {
		cfg := numerator.DefaultConfig("CP")
		number, err := p.numerator.GetNextNumber(ctx, cfg, event.Timestamp)
		if err != nil {
			logger.Warn(ctx, "numerator failed for crypto payment, using fallback",
				"error", err,
				"tx_hash", event.TxHash,
			)
			payment.Number = fmt.Sprintf("CP-%s", id.Compact(id.New()))
		} else {
			payment.Number = number
		}
	} else {
		payment.Number = fmt.Sprintf("CP-%s", id.Compact(id.New()))
	}

	if err := p.paymentRepo.Create(ctx, payment); err != nil {
		return fmt.Errorf("create payment for tx %s: %w", event.TxHash, err)
	}
	if invoice.SinglePayment {
		invoice.SelectedPaymentID = &payment.ID
		if err := p.invoiceSvc.Update(ctx, invoice); err != nil {
			return fmt.Errorf("select first invoice payment: %w", err)
		}
	}
	if err := p.publishPaymentLifecycle(ctx, WebhookPaymentDetected, payment, event); err != nil {
		return err
	}
	if inboxRow != nil && p.inbox != nil {
		var allocationID id.ID
		if alloc != nil {
			allocationID = alloc.ID
		}
		if err := p.inbox.MarkMatched(ctx, inboxRow.ID, invoice.ID, payment.ID, allocationID); err != nil {
			return err
		}
	}

	logger.Info(ctx, "payment created",
		"payment_id", payment.ID,
		"invoice_id", invoice.ID,
		"tx_hash", event.TxHash,
		"amount", event.Amount.String(),
	)

	// Step 6: derive the invoice projection from the newly persisted canonical
	// payment fact. Invoice state has exactly one writer.
	if _, err := p.invoiceLifecycle.SyncInTransaction(ctx, invoice.ID); err != nil {
		return fmt.Errorf("synchronize invoice amount: %w", err)
	}

	// Step 7: Process confirmations for the new payment
	return p.processConfirmations(ctx, payment, event)
}

func (p *EventProcessor) handleProvenReplacement(ctx context.Context, previous *crypto_payment.CryptoPayment, replacement BlockchainEvent) error {
	locked, err := p.paymentRepo.GetByIDForUpdate(ctx, previous.ID)
	if err != nil {
		return fmt.Errorf("lock replaced payment %s: %w", previous.ID, err)
	}
	locked.Canonicality = string(CanonicalityOrphaned)
	locked.ReplacementTxHash = replacement.TxHash
	if locked.Posted || locked.Status == crypto_payment.PaymentStatusConfirmed || locked.Status == crypto_payment.PaymentStatusSettled {
		if p.financialReorgs == nil {
			return fmt.Errorf("posted payment %s was replaced but financial reversal is not configured", locked.ID)
		}
		if err := p.financialReorgs.RecordFinancialReorg(ctx, locked, replacement); err != nil {
			return fmt.Errorf("compensate replaced payment: %w", err)
		}
		locked.MarkUnposted()
	}
	if err := p.fsm.Transition(ctx, locked, crypto_payment.PaymentStatusReplaced, "proven_nonce_replacement", TransitionMetadata{
		TxHash: replacement.TxHash, BlockNumber: replacement.BlockNumber,
	}); err != nil {
		return err
	}
	if err := p.publishPaymentLifecycle(ctx, WebhookPaymentReplaced, locked, replacement); err != nil {
		return err
	}
	if err := p.clearSelectedPayment(ctx, locked.InvoiceID, locked.ID); err != nil {
		return err
	}
	_, err = p.invoiceLifecycle.SyncInTransaction(ctx, locked.InvoiceID)
	if err != nil {
		return err
	}
	return p.promoteSinglePaymentReview(ctx, locked.InvoiceID)
}

// handleConfirmationUpdate processes confirmation updates for existing payments.
func (p *EventProcessor) handleConfirmationUpdate(
	ctx context.Context,
	payment *crypto_payment.CryptoPayment,
	event BlockchainEvent,
) error {
	// Handle reorg
	if event.EventType == EventTypeReorg {
		return p.handleReorg(ctx, payment, event)
	}
	// A delayed latest observation cannot weaken already credited evidence.
	if payment.Posted && event.Finality == FinalityProvisional {
		return nil
	}
	if event.Amount.IsPositive() && !event.Amount.Equal(payment.Amount) {
		return ErrVerificationMismatch
	}

	// A canonical reappearance reactivates the same payment identity. The
	// provisional invoice amount is restored exactly once before confirmations
	// resume, so a reorg can never create a second payment.
	if payment.Status == crypto_payment.PaymentStatusReorged {
		if event.EventType != EventTypeTransfer {
			return nil
		}
		if !event.Amount.Equal(payment.Amount) {
			return fmt.Errorf("reappeared payment %s amount changed from %s to %s", payment.ID, payment.Amount, event.Amount)
		}
		payment.Confirmations = event.Confirmations
		payment.BlockNumber = event.BlockNumber
		payment.BlockHash = event.BlockHash
		payment.FinalityStatus = string(event.Finality)
		payment.Canonicality = string(CanonicalityCanonical)
		if err := p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusDetected,
			"canonical_reappearance", TransitionMetadata{BlockNumber: event.BlockNumber, TxHash: event.TxHash}); err != nil {
			return fmt.Errorf("reactivate reorged payment: %w", err)
		}
		if _, err := p.invoiceLifecycle.SyncInTransaction(ctx, payment.InvoiceID); err != nil {
			return fmt.Errorf("restore invoice projection after reorg: %w", err)
		}
	}

	// Merge confirmation metadata monotonically. Reorgs have a separate,
	// evidence-checked path; ordinary provisional observations cannot demote it.
	changed := false
	if event.Confirmations > payment.Confirmations || (event.VerificationID != nil && !payment.Posted && event.Confirmations != payment.Confirmations) {
		payment.Confirmations = event.Confirmations
		changed = true
	}
	if event.BlockHash != "" && event.BlockHash != payment.BlockHash {
		if payment.Posted {
			return ErrVerificationMismatch
		}
		payment.BlockHash = event.BlockHash
		payment.BlockNumber = event.BlockNumber
		changed = true
	}
	if event.Finality != "" && event.Finality != FinalityStatus(payment.FinalityStatus) && (event.Finality != FinalityProvisional || payment.FinalityStatus == string(FinalityProvisional)) {
		payment.FinalityStatus = string(event.Finality)
		changed = true
	}
	if event.Canonicality == CanonicalityCanonical && payment.Canonicality != string(CanonicalityCanonical) {
		payment.Canonicality = string(event.Canonicality)
		changed = true
	}
	if changed {
		if err := p.paymentRepo.Update(ctx, payment); err != nil {
			return fmt.Errorf("update payment evidence: %w", err)
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
		if err := p.publishPaymentLifecycle(ctx, WebhookPaymentConfirming, payment, event); err != nil {
			return err
		}
	}

	// Step 2: Confirming → Confirmed (reached required confirmations)
	if payment.Status == crypto_payment.PaymentStatusConfirming && payment.Canonicality == string(CanonicalityCanonical) &&
		(event.Finality == FinalitySolidified || event.Finality == FinalityFinalized) {
		if p.financialGuard != nil {
			proofID, err := p.financialGuard.Authorize(ctx, payment, event)
			if errors.Is(err, ErrVerificationPending) {
				return nil
			}
			if err != nil {
				return err
			}
			if !id.IsNil(proofID) {
				payment.VerificationID = &proofID
			}
		}
		// The custody of the final transfer is a receipt; the payment only
		// attributes it to the merchant.
		if p.receipts == nil {
			return fmt.Errorf("payment %s reached finality but receipt recorder is not configured", payment.ID)
		}
		proven := event
		proven.VerificationID = payment.VerificationID
		receiptID, err := p.receipts.RecordPaymentReceipt(ctx, payment, proven)
		if err != nil {
			return fmt.Errorf("record payment receipt: %w", err)
		}
		if payment.ReceiptID != nil && *payment.ReceiptID != receiptID {
			return fmt.Errorf("payment %s attributes receipt %s, chain effect resolved to %s", payment.ID, *payment.ReceiptID, receiptID)
		}
		payment.ReceiptID = &receiptID
		// Use postingEngine to generate and record register movements.
		// The engine internally sets payment.Posted = true and increments PostedVersion.
		// We pass fsm.Transition as the update callback to eliminate double-writes:
		// the FSM transition will persist the new Status and the updated Posted fields in one UPDATE.
		if err := p.postingEngine.Post(ctx, payment, func(ctx context.Context) error {
			return p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusConfirmed,
				"confirmed", TransitionMetadata{
					Confirmations:  event.Confirmations,
					RequiredConfs:  payment.RequiredConfs,
					VerificationID: payment.VerificationID,
				})
		}); err != nil {
			return fmt.Errorf("post payment: %w", err)
		}
		if err := p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusSettled,
			"merchant_ledger_credited", TransitionMetadata{
				Confirmations:  event.Confirmations,
				RequiredConfs:  payment.RequiredConfs,
				VerificationID: payment.VerificationID,
			}); err != nil {
			return fmt.Errorf("settle payment to merchant ledger: %w", err)
		}
		if err := p.publishPaymentLifecycle(ctx, WebhookPaymentConfirmed, payment, event); err != nil {
			return err
		}

		invoice, err := p.invoiceSvc.GetByID(ctx, payment.InvoiceID)
		if err != nil {
			return fmt.Errorf("lock invoice %s after post: %w", payment.InvoiceID, err)
		}
		if InvoiceIsLateTerminal(invoice.Status) {
			if err := p.publishInvoiceLifecycle(ctx, WebhookInvoiceLatePayment, invoice, map[string]any{
				"txHash":     event.TxHash,
				"paymentId":  payment.ID.String(),
				"lateReason": string(invoice.Status),
			}); err != nil {
				return err
			}
			if p.onLatePayment != nil {
				p.onLatePayment(ctx)
			}
			return nil
		}

		invoiceCompleted, err := p.syncInvoiceAfterFinality(ctx, payment.InvoiceID)
		if err != nil {
			return fmt.Errorf("advance invoice %s after finality: %w", payment.InvoiceID, err)
		}
		if invoiceCompleted {
			if err := p.handleWalletAfterConfirm(ctx, payment); err != nil {
				return fmt.Errorf("handle wallet after confirm: %w", err)
			}
		}
	}

	return nil
}

func (p *EventProcessor) handleReorg(ctx context.Context, payment *crypto_payment.CryptoPayment, event BlockchainEvent) error {
	if p.financialGuard != nil {
		proofID, err := p.financialGuard.AuthorizeReorg(ctx, payment, event)
		if errors.Is(err, ErrVerificationPending) {
			return nil
		}
		if err != nil {
			return err
		}
		if !id.IsNil(proofID) {
			event.VerificationID = &proofID
		}
	}
	if payment.Status == crypto_payment.PaymentStatusReorged {
		return nil
	}
	payment.Canonicality = string(CanonicalityOrphaned)
	payment.Confirmations = 0
	payment.FinalityStatus = string(FinalityProvisional)

	if payment.Posted || payment.Status == crypto_payment.PaymentStatusConfirmed || payment.Status == crypto_payment.PaymentStatusSettled {
		if p.financialReorgs == nil {
			return fmt.Errorf("posted payment %s was orphaned but financial reorg recorder is not configured", payment.ID)
		}
		if err := p.financialReorgs.RecordFinancialReorg(ctx, payment, event); err != nil {
			return fmt.Errorf("record financial reorg incident: %w", err)
		}
		payment.MarkUnposted()
		if err := p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusReorged,
			"posted_chain_reorg", TransitionMetadata{BlockNumber: event.BlockNumber, TxHash: event.TxHash, VerificationID: event.VerificationID}); err != nil {
			return fmt.Errorf("transition posted payment to reorged: %w", err)
		}
		if err := p.publishPaymentLifecycle(ctx, WebhookPaymentReorged, payment, event); err != nil {
			return err
		}
		return p.syncInvoiceAfterReorg(ctx, payment.InvoiceID)
	}

	if err := p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusReorged,
		"chain_reorg", TransitionMetadata{BlockNumber: event.BlockNumber, TxHash: event.TxHash, VerificationID: event.VerificationID}); err != nil {
		return err
	}
	if err := p.publishPaymentLifecycle(ctx, WebhookPaymentReorged, payment, event); err != nil {
		return err
	}
	if err := p.clearSelectedPayment(ctx, payment.InvoiceID, payment.ID); err != nil {
		return err
	}
	_, err := p.invoiceLifecycle.SyncInTransaction(ctx, payment.InvoiceID)
	if err != nil {
		return err
	}
	return p.promoteSinglePaymentReview(ctx, payment.InvoiceID)
}

func (p *EventProcessor) clearSelectedPayment(ctx context.Context, invoiceID, paymentID id.ID) error {
	invoice, err := p.invoiceSvc.GetByIDForUpdate(ctx, invoiceID)
	if err != nil {
		return err
	}
	if !invoice.SinglePayment || invoice.SelectedPaymentID == nil || *invoice.SelectedPaymentID != paymentID {
		return nil
	}
	if InvoiceIsLateTerminal(invoice.Status) || invoice.Status == crypto_invoice.InvoiceStatusCompleted || invoice.Status == crypto_invoice.InvoiceStatusConfirmed {
		return nil
	}
	invoice.SelectedPaymentID = nil
	return p.invoiceSvc.Update(ctx, invoice)
}

func (p *EventProcessor) promoteSinglePaymentReview(ctx context.Context, invoiceID id.ID) error {
	if p.inbox == nil {
		return nil
	}
	invoice, err := p.invoiceSvc.GetByIDForUpdate(ctx, invoiceID)
	if err != nil {
		return err
	}
	if !invoice.SinglePayment || invoice.SelectedPaymentID != nil ||
		(invoice.Status != crypto_invoice.InvoiceStatusCreated && invoice.Status != crypto_invoice.InvoiceStatusPartiallyPaid) {
		return nil
	}
	row, err := p.inbox.NextSinglePaymentReview(ctx, invoiceID)
	if err != nil || row == nil {
		return err
	}
	event, err := RebuildBlockchainEvent(*row)
	if err != nil {
		return fmt.Errorf("rebuild next single-payment candidate: %w", err)
	}
	return p.processEventInTx(ctx, event)
}

func (p *EventProcessor) syncInvoiceAfterReorg(ctx context.Context, invoiceID id.ID) error {
	_, err := p.invoiceLifecycle.SyncInTransaction(ctx, invoiceID)
	return err
}

// syncInvoiceAfterFinality derives the invoice projection from canonical payment
// facts. Confirmed and completed are separate transitions even when both are
// committed by the same database transaction.
func (p *EventProcessor) syncInvoiceAfterFinality(ctx context.Context, invoiceID id.ID) (bool, error) {
	return p.invoiceLifecycle.SyncInTransaction(ctx, invoiceID)
}

func (p *EventProcessor) ReconcileInvoice(ctx context.Context, invoiceID id.ID) (bool, error) {
	var closed bool
	err := p.txManager.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		closed, err = p.invoiceLifecycle.SyncInTransaction(txCtx, invoiceID)
		if err != nil {
			return err
		}
		invoice, err := p.invoiceSvc.GetByIDForUpdate(txCtx, invoiceID)
		if err != nil {
			return err
		}
		if !invoice.SinglePayment || invoice.SelectedPaymentID == nil ||
			(invoice.Status != crypto_invoice.InvoiceStatusUnderpaid && invoice.Status != crypto_invoice.InvoiceStatusCompleted) ||
			invoice.WalletID == nil {
			return nil
		}
		w, err := p.walletSvc.GetByID(txCtx, *invoice.WalletID)
		if err != nil {
			return err
		}
		if w.Status != wallet.WalletStatusLeased {
			return nil
		}
		payment, err := p.paymentRepo.GetByIDForUpdate(txCtx, *invoice.SelectedPaymentID)
		if err != nil {
			return err
		}
		if payment.Posted && payment.Status == crypto_payment.PaymentStatusSettled {
			return p.handleWalletAfterConfirm(txCtx, payment)
		}
		return nil
	})
	return closed, err
}

func (p *EventProcessor) ExpireInvoiceInTransaction(ctx context.Context, invoiceID id.ID) (bool, error) {
	return p.invoiceLifecycle.ExpireInTransaction(ctx, invoiceID)
}

// createTopUpInvoice generates a system CryptoInvoice for an incoming payment to a persistent wallet.
func (p *EventProcessor) createTopUpInvoice(ctx context.Context, w *wallet.Wallet, event BlockchainEvent, tok *token.Token) (*crypto_invoice.CryptoInvoice, error) {
	if tok == nil {
		return nil, fmt.Errorf("resolved top-up token is required")
	}
	invoice := crypto_invoice.NewCryptoInvoice(
		*w.MerchantID,
		tok.ID,
		event.Amount,
	)

	invoice.WalletID = &w.ID
	invoice.CreationSource = crypto_invoice.CreationSourceSystem
	invoice.InvoiceKind = crypto_invoice.InvoiceKindTopUp
	invoice.CustomerID = w.CustomerID
	// The lifecycle service will derive Paid after the payment fact is persisted.
	invoice.Status = crypto_invoice.InvoiceStatusCreated

	// Assign a numerator if available
	if p.numerator != nil {
		cfg := numerator.DefaultConfig("CI")
		number, err := p.numerator.GetNextNumber(ctx, cfg, p.nowFunc())
		if err == nil {
			invoice.Number = number
		} else {
			invoice.Number = fmt.Sprintf("CI-%s", id.Compact(id.New()))
		}
	} else {
		invoice.Number = fmt.Sprintf("CI-%s", id.Compact(id.New()))
	}

	if err := p.invoiceSvc.Create(ctx, invoice); err != nil {
		return nil, fmt.Errorf("save top-up invoice: %w", err)
	}

	logger.Info(ctx, "created top-up invoice for persistent wallet",
		"invoice_id", invoice.ID,
		"wallet_id", w.ID,
		"customer_id", w.CustomerID,
		"amount", event.Amount.String(),
	)

	return invoice, nil
}

func (p *EventProcessor) publishInvoiceLifecycle(
	ctx context.Context,
	event WebhookEventType,
	invoice *crypto_invoice.CryptoInvoice,
	extra map[string]any,
) error {
	if p.webhookPublisher == nil {
		return nil
	}
	data := InvoiceWebhookData(invoice, extra)
	invoiceID := invoice.ID
	if err := p.webhookPublisher.PublishLifecycleWebhook(ctx, LifecycleWebhookEvent{
		Event:           event,
		AggregateID:     invoice.ID,
		MerchantID:      invoice.MerchantID,
		InvoiceID:       &invoiceID,
		ResourceVersion: invoice.Version,
		OccurredAt:      p.nowFunc(),
		Data:            data,
	}); err != nil {
		return fmt.Errorf("publish %s webhook for invoice %s: %w", event, invoice.ID, err)
	}
	return nil
}

func (p *EventProcessor) publishPaymentLifecycle(ctx context.Context, eventType WebhookEventType, payment *crypto_payment.CryptoPayment, event BlockchainEvent) error {
	if p.webhookPublisher == nil {
		return nil
	}
	invoiceID := payment.InvoiceID
	return p.webhookPublisher.PublishLifecycleWebhook(ctx, LifecycleWebhookEvent{
		Event:           eventType,
		AggregateID:     payment.ID,
		MerchantID:      payment.MerchantID,
		InvoiceID:       &invoiceID,
		ResourceVersion: payment.Version,
		OccurredAt:      p.nowFunc().UTC(),
		Data: map[string]any{
			"paymentId":             payment.ID.String(),
			"invoiceId":             payment.InvoiceID.String(),
			"status":                payment.Status,
			"amount":                payment.Amount.String(),
			"txHash":                payment.TxHash,
			"eventIndex":            payment.EventIndex,
			"sourceAddress":         payment.SourceAddress,
			"sourceNonce":           payment.SourceNonce,
			"replacementTxHash":     payment.ReplacementTxHash,
			"confirmations":         event.Confirmations,
			"requiredConfirmations": payment.RequiredConfs,
			"canonicality":          payment.Canonicality,
			"finalityStatus":        payment.FinalityStatus,
		},
	})
}

func (p *EventProcessor) persistTransferInbox(
	ctx context.Context,
	event BlockchainEvent,
	walletID *id.ID,
	tokenID *id.ID,
) (*InboundChainEvent, error) {
	if p.inbox == nil {
		return nil, nil
	}
	row, err := NewInboundChainEvent(event, walletID, tokenID)
	if err != nil {
		return nil, err
	}
	stored, _, err := p.inbox.Upsert(ctx, row)
	if err != nil {
		return nil, err
	}
	return &stored, nil
}

func (p *EventProcessor) markInboxTerminal(ctx context.Context, event BlockchainEvent, status InboxMatchStatus, reason string, details map[string]any) error {
	if p.inbox == nil {
		return nil
	}
	stored, err := p.persistTransferInbox(ctx, event, nil, nil)
	if err != nil {
		return err
	}
	return p.markInboxRowTerminal(ctx, stored, status, reason, details)
}

func (p *EventProcessor) markInboxRowTerminal(ctx context.Context, row *InboundChainEvent, status InboxMatchStatus, reason string, details map[string]any) error {
	if p.inbox == nil || row == nil {
		return nil
	}
	return p.inbox.MarkTerminal(ctx, row.ID, status, reason, details)
}

func (p *EventProcessor) matchInvoiceForTransfer(
	ctx context.Context,
	w *wallet.Wallet,
	event BlockchainEvent,
	eventToken *token.Token,
) (*crypto_invoice.CryptoInvoice, *WalletAllocation, error) {
	if w.IsPersistent() {
		invoice, err := p.findActiveInvoice(ctx, w)
		if err != nil {
			invoice, err = p.createTopUpInvoice(ctx, w, event, eventToken)
			if err != nil {
				return nil, nil, fmt.Errorf("create top-up invoice: %w", err)
			}
		}
		return invoice, nil, nil
	}

	if p.allocations != nil {
		covering, err := p.allocations.FindCovering(ctx, w.ID, event.Timestamp)
		if err != nil {
			return nil, nil, err
		}
		open, err := p.allocations.GetOpenByWallet(ctx, w.ID)
		if err != nil {
			return nil, nil, err
		}
		decision := DecideMatch(open, covering, eventToken.ID, event.Timestamp)
		switch decision.Status {
		case MatchNeedsReview:
			if err := p.markInboxTerminal(ctx, event, InboxNeedsReview, decision.Reason, nil); err != nil {
				return nil, nil, err
			}
			return nil, nil, nil
		case MatchUnmatched:
			if err := p.markInboxTerminal(ctx, event, InboxUnmatched, decision.Reason, nil); err != nil {
				return nil, nil, err
			}
			return nil, nil, nil
		case MatchMatched:
			invoice, err := p.invoiceSvc.GetByID(ctx, decision.Allocation.InvoiceID)
			if err != nil {
				return nil, nil, fmt.Errorf("get covering invoice %s: %w", decision.Allocation.InvoiceID, err)
			}
			return invoice, decision.Allocation, nil
		}
	}

	invoice, err := p.findActiveInvoice(ctx, w)
	if err != nil {
		logger.Warn(ctx, "no active invoice for wallet",
			"wallet_id", w.ID,
			"address", w.Address,
			"tx_hash", event.TxHash,
		)
		if markErr := p.markInboxTerminal(ctx, event, InboxUnmatched, MatchReasonNoAllocation, nil); markErr != nil {
			return nil, nil, markErr
		}
		return nil, nil, nil
	}
	if invoice.TokenID != eventToken.ID {
		if markErr := p.markInboxTerminal(ctx, event, InboxUnmatched, MatchReasonWrongToken, nil); markErr != nil {
			return nil, nil, markErr
		}
		return nil, nil, nil
	}
	return invoice, nil, nil
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

	if invoice.Status == crypto_invoice.InvoiceStatusExpired ||
		invoice.Status == crypto_invoice.InvoiceStatusCancelled {
		return nil, fmt.Errorf("invoice %s is %s (not active)", invoice.ID, invoice.Status)
	}
	return invoice, nil
}

// handleWalletAfterConfirm decides wallet status after a payment is confirmed.
//
// Logic:
//   - threshold=0 → sweep immediately
//   - threshold>0, transient wallet → release to free (available for new invoices);
//     sweep evaluation job will handle sweep_pending when balance reaches threshold
//   - persistent wallet → never released (stays assigned to customer)
func (p *EventProcessor) handleWalletAfterConfirm(ctx context.Context, payment *crypto_payment.CryptoPayment) error {
	nextStatus := wallet.WalletStatusSweepPending
	if p.sweepResolver != nil {
		sweepCfg, err := p.sweepResolver.Resolve(ctx, payment.MerchantID, payment.TokenID)
		if err != nil {
			logger.Warn(ctx, "sweep config resolve failed, using immediate sweep policy",
				"merchant_id", payment.MerchantID,
				"token_id", payment.TokenID,
				"error", err,
			)
		} else if sweepCfg.IsZeroThreshold() {
			if !sweepCfg.SweepableAmount(payment.Amount).IsPositive() {
				nextStatus = wallet.WalletStatusFree
			}
		} else {
			nextStatus = wallet.WalletStatusFree
		}
	}

	w, err := p.walletSvc.GetByID(ctx, payment.WalletID)
	if err != nil {
		return fmt.Errorf("get wallet %s: %w", payment.WalletID, err)
	}
	if !w.IsTransient() {
		logger.Debug(ctx, "persistent wallet stays assigned after confirm",
			"wallet_id", w.ID,
			"customer_id", w.CustomerID,
		)
		return nil
	}

	if p.leaseCloser != nil {
		return p.leaseCloser.CloseTransientLease(ctx, payment.WalletID, AllocationCloseConfirmed, nextStatus)
	}
	if nextStatus == wallet.WalletStatusSweepPending {
		return p.walletSvc.MarkSweepPending(ctx, payment.WalletID)
	}
	return p.walletSvc.ReleaseWallet(ctx, payment.WalletID)
}
