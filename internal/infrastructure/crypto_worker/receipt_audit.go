package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
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
		if err := r.audits.MarkOrphaned(ctx, task, proofID); err != nil {
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
