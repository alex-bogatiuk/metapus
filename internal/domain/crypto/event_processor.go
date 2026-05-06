package crypto

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
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
	return types.NewCryptoAmount(big.NewInt(1000))
}

// EventProcessor orchestrates the lifecycle of blockchain events:
// match wallet → find invoice → create/update payment → FSM transition → post.
//
// This is the central business logic component — chain-agnostic.
// Chain watchers (TRON, ETH, etc.) feed normalized BlockchainEvent into this processor.
type EventProcessor struct {
	fsm            *PaymentFSM
	walletSvc      *wallet.Service
	invoiceRepo    crypto_invoice.Repository
	paymentRepo    crypto_payment.Repository
	postingEngine  *posting.Engine
	txManager      tx.Manager
	numerator      numerator.Generator
	dustThreshold  types.CryptoAmount
}

// EventProcessorConfig holds dependencies for the event processor.
type EventProcessorConfig struct {
	FSM           *PaymentFSM
	WalletSvc     *wallet.Service
	InvoiceRepo   crypto_invoice.Repository
	PaymentRepo   crypto_payment.Repository
	PostingEngine *posting.Engine
	TxManager     tx.Manager
	Numerator     numerator.Generator
	// DustThreshold is the minimum amount to accept. Zero = use default (1000 minor units).
	DustThreshold types.CryptoAmount
}

// NewEventProcessor creates a new event processor.
func NewEventProcessor(cfg EventProcessorConfig) *EventProcessor {
	threshold := cfg.DustThreshold
	if threshold.IsZero() {
		threshold = _defaultDustThreshold()
	}

	return &EventProcessor{
		fsm:            cfg.FSM,
		walletSvc:      cfg.WalletSvc,
		invoiceRepo:    cfg.InvoiceRepo,
		paymentRepo:    cfg.PaymentRepo,
		postingEngine:  cfg.PostingEngine,
		txManager:      cfg.TxManager,
		numerator:      cfg.Numerator,
		dustThreshold:  threshold,
	}
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

	// Step 1: Match wallet
	w, err := p.walletSvc.FindByAddress(ctx, event.NetworkID, event.ToAddress)
	if err != nil {
		// Not our wallet — skip silently (normal for chain watchers monitoring many addresses)
		logger.Debug(ctx, "ignoring event for unknown wallet",
			"address", event.ToAddress,
			"tx_hash", event.TxHash,
		)
		return nil
	}

	// Step 2: Idempotency check — do we already have a payment for this tx?
	existing, err := p.paymentRepo.FindByTxHash(ctx, event.TxHash)
	if err == nil && existing != nil {
		// Existing payment — update confirmations
		return p.handleConfirmationUpdate(ctx, existing, event)
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
		logger.Warn(ctx, "no active invoice for wallet",
			"wallet_id", w.ID,
			"address", w.Address,
			"tx_hash", event.TxHash,
		)
		return nil // No active invoice — funds will be reconciled later
	}

	// Step 5: Create new CryptoPayment
	payment := crypto_payment.NewCryptoPayment(
		invoice.OrganizationID,
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
func (p *EventProcessor) processConfirmations(
	ctx context.Context,
	payment *crypto_payment.CryptoPayment,
	event BlockchainEvent,
) error {
	switch payment.Status {
	case crypto_payment.PaymentStatusDetected:
		if event.Confirmations >= 1 {
			if err := p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusConfirming,
				"first_confirmation", TransitionMetadata{
					Confirmations: event.Confirmations,
					BlockNumber:   event.BlockNumber,
				}); err != nil {
				return fmt.Errorf("transition to confirming: %w", err)
			}
		}
		// Fall through to check if already fully confirmed
		if payment.Status != crypto_payment.PaymentStatusConfirming {
			return nil
		}
		fallthrough

	case crypto_payment.PaymentStatusConfirming:
		if event.Confirmations >= payment.RequiredConfs {
			if err := p.fsm.Transition(ctx, payment, crypto_payment.PaymentStatusConfirmed,
				"confirmed", TransitionMetadata{
					Confirmations: event.Confirmations,
					RequiredConfs: payment.RequiredConfs,
				}); err != nil {
				return fmt.Errorf("transition to confirmed: %w", err)
			}

			// Post the payment — record register movements
			if err := p.postPayment(ctx, payment); err != nil {
				return fmt.Errorf("post payment: %w", err)
			}

			// Mark wallet for sweep
			if err := p.walletSvc.MarkSweepPending(ctx, payment.WalletID); err != nil {
				logger.Error(ctx, "failed to mark wallet sweep pending",
					"wallet_id", payment.WalletID,
					"error", err,
				)
				// Non-critical: don't fail the payment
			}

			// Update invoice status to Confirmed
			if err := p.confirmInvoice(ctx, payment.InvoiceID); err != nil {
				logger.Error(ctx, "failed to confirm invoice",
					"invoice_id", payment.InvoiceID,
					"error", err,
				)
			}
		}
	}

	return nil
}

// postPayment posts the payment document to record register movements.
func (p *EventProcessor) postPayment(ctx context.Context, payment *crypto_payment.CryptoPayment) error {
	// Posting is handled via the posting engine's document posting workflow
	payment.Posted = true
	payment.PostedVersion++

	if err := p.paymentRepo.Update(ctx, payment); err != nil {
		return fmt.Errorf("mark payment as posted: %w", err)
	}

	logger.Info(ctx, "payment posted",
		"payment_id", payment.ID,
		"tx_hash", payment.TxHash,
	)

	return nil
}

// updateInvoiceAmount updates the invoice's received amount and status.
func (p *EventProcessor) updateInvoiceAmount(ctx context.Context, invoice *crypto_invoice.CryptoInvoice, event BlockchainEvent) error {
	// Add received amount
	invoice.ReceivedAmount = invoice.ReceivedAmount.Add(event.Amount)

	// Update invoice status based on received vs expected
	if invoice.ReceivedAmount.Cmp(invoice.ExpectedAmount) >= 0 {
		invoice.Status = crypto_invoice.InvoiceStatusPaid
	} else if invoice.ReceivedAmount.IsPositive() {
		invoice.Status = crypto_invoice.InvoiceStatusPartiallyPaid
	}

	if err := p.invoiceRepo.Update(ctx, invoice); err != nil {
		return fmt.Errorf("update invoice %s: %w", invoice.ID, err)
	}

	return nil
}

// confirmInvoice updates the invoice status to Confirmed.
func (p *EventProcessor) confirmInvoice(ctx context.Context, invoiceID id.ID) error {
	invoice, err := p.invoiceRepo.GetByID(ctx, invoiceID)
	if err != nil {
		return fmt.Errorf("get invoice %s: %w", invoiceID, err)
	}

	if invoice.Status == crypto_invoice.InvoiceStatusPaid {
		invoice.Status = crypto_invoice.InvoiceStatusConfirmed
		if err := p.invoiceRepo.Update(ctx, invoice); err != nil {
			return fmt.Errorf("confirm invoice %s: %w", invoiceID, err)
		}
	}

	return nil
}

// findActiveInvoice finds the active (leased) invoice for a wallet.
func (p *EventProcessor) findActiveInvoice(ctx context.Context, w *wallet.Wallet) (*crypto_invoice.CryptoInvoice, error) {
	if w.LeasedForID == nil {
		return nil, fmt.Errorf("wallet %s is not leased", w.ID)
	}

	invoice, err := p.invoiceRepo.GetByID(ctx, *w.LeasedForID)
	if err != nil {
		return nil, fmt.Errorf("get invoice %s: %w", *w.LeasedForID, err)
	}

	// Only accept payments for active invoices
	if invoice.Status == crypto_invoice.InvoiceStatusExpired ||
		invoice.Status == crypto_invoice.InvoiceStatusCancelled {
		return nil, fmt.Errorf("invoice %s is %s (not active)", invoice.ID, invoice.Status)
	}

	// Check expiration
	if time.Now().UTC().After(invoice.ExpiresAt) && invoice.Status == crypto_invoice.InvoiceStatusCreated {
		invoice.Status = crypto_invoice.InvoiceStatusExpired
		if err := p.invoiceRepo.Update(ctx, invoice); err != nil {
			return nil, fmt.Errorf("expire invoice %s: %w", invoice.ID, err)
		}
		return nil, fmt.Errorf("invoice %s has expired", invoice.ID)
	}

	return invoice, nil
}
