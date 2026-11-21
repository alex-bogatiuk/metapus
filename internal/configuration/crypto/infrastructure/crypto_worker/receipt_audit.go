package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/id"
)

const (
	_receiptAuditPeriod     = time.Minute
	_receiptAuditBatchLimit = 100
	_receiptAuditLease      = 2 * time.Minute
)

func (p *CryptoProcessor) runReceiptAuditLoop(ctx context.Context) {
	if p.receipts == nil {
		return
	}
	ticker := time.NewTicker(_receiptAuditPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.drivers == nil {
				continue
			}
			if _, err := p.receipts.auditCanonicality(ctx, p.drivers, p.workerID); err != nil && ctx.Err() == nil {
				p.log.Errorw("custody receipt canonicality audit failed", "error", err)
			}
		}
	}
}

// auditCanonicality re-proves accounted receipts and returns how many were
// found orphaned in this pass. Individual read failures are scheduled for retry.
func (r *receiptIntake) auditCanonicality(ctx context.Context, drivers *crypto.ChainDriverRegistry, workerID string) (int, error) {
	tasks, err := r.audits.Claim(ctx, workerID, _receiptAuditBatchLimit, _receiptAuditLease)
	if err != nil {
		return 0, err
	}
	orphaned := 0
	for _, task := range tasks {
		found, checkErr := r.auditReceipt(ctx, drivers, task)
		if found {
			orphaned++
			continue
		}
		if err := r.audits.Release(ctx, task.ReceiptID, workerID, checkErr); err != nil {
			return orphaned, err
		}
	}
	return orphaned, nil
}

// orphan compensates the posted payments that attribute the receipt and marks
// the receipt orphaned in one transaction: the merchant never keeps a credit
// for a transfer the audit proved absent, and a failure retries both.
func (r *receiptIntake) orphan(ctx context.Context, task crypto_repo.ReceiptAuditTask, proofID id.ID, event crypto.BlockchainEvent) error {
	payments, err := r.audits.AttributingPayments(ctx, task.ReceiptID)
	if err != nil {
		return err
	}
	for _, paymentID := range payments {
		if r.paymentReorg == nil {
			return fmt.Errorf("orphaned receipt %s is attributed by posted payment %s without a payment reorg handler", task.ReceiptID, paymentID)
		}
		if err := r.paymentReorg(ctx, paymentID, task, event); err != nil {
			return fmt.Errorf("compensate payment %s of orphaned receipt: %w", paymentID, err)
		}
	}
	return r.audits.MarkOrphaned(ctx, task, proofID)
}

// reorgReceiptPayment routes the receipt absence proof to the payment through
// the event processor, which compensates its attribution and holds the network.
func (p *CryptoProcessor) reorgReceiptPayment(ctx context.Context, paymentID id.ID, task crypto_repo.ReceiptAuditTask, event crypto.BlockchainEvent) error {
	payment, err := p.paymentRepo.GetByID(ctx, paymentID)
	if err != nil {
		return err
	}
	if payment.NetworkID != task.NetworkID || !strings.EqualFold(payment.TxHash, task.TxHash) ||
		payment.EventKind != task.EventKind || payment.EventIndex != task.EventIndex ||
		!strings.EqualFold(payment.BlockHash, task.BlockHash) {
		return fmt.Errorf("payment %s is not anchored to the audited receipt transfer", paymentID)
	}
	return p.eventProcessor.ProcessEvent(ctx, event)
}

func (r *receiptIntake) auditReceipt(ctx context.Context, drivers *crypto.ChainDriverRegistry, task crypto_repo.ReceiptAuditTask) (bool, error) {
	driver, err := drivers.Resolve(task.NetworkID)
	if err != nil {
		return false, fmt.Errorf("resolve receipt network: %w", err)
	}
	verifier, ok := driver.(crypto.TransferVerifier)
	if !ok || !verifier.VerificationRequired() {
		return false, errors.New("network has no independent transfer proof")
	}
	expected, err := r.proofs.ExpectedAccountingTransfer(ctx, task.Basis())
	if err != nil {
		return false, err
	}
	proof, err := verifier.VerifyTransfer(ctx, expected)
	if err != nil {
		if errors.Is(err, crypto.ErrVerificationMismatch) {
			return false, errors.Join(err, r.proofs.ReportIngestionFailure(ctx, task.NetworkID, "canonicality", task.BlockNumber, "transfer_mismatch", true))
		}
		return false, err
	}
	if proof.Event.Canonicality == crypto.CanonicalityOrphaned {
		proofID, err := r.proofs.SaveVerification(ctx, proof)
		if err != nil {
			return false, err
		}
		event := proof.Event
		event.VerificationID, event.EventType = &proofID, crypto.EventTypeReorg
		if err := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
			return r.orphan(txCtx, task, proofID, event)
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	if proof.Event.BlockHash != task.BlockHash || proof.Event.BlockNumber != task.BlockNumber {
		// Re-included in another block: the money may still be there, so the
		// network is held for investigation and the audit keeps retrying.
		return false, errors.Join(crypto.ErrVerificationMismatch,
			r.proofs.ReportIngestionFailure(ctx, task.NetworkID, "canonicality", task.BlockNumber, "solidified_payment_changed", true))
	}
	return false, nil
}
