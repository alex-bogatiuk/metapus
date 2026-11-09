package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

const canonicalityAuditPeriod = time.Minute
const canonicalityAuditBatchSize = 100

func (p *CryptoProcessor) runCanonicalityAuditLoop(ctx context.Context) {
	ticker := time.NewTicker(canonicalityAuditPeriod)
	defer ticker.Stop()
	for {
		if err := p.pollCanonicalityAudits(ctx); err != nil && ctx.Err() == nil {
			p.log.Errorw("settled payment canonicality audit failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *CryptoProcessor) pollCanonicalityAudits(ctx context.Context) error {
	payments, err := p.paymentRepo.ClaimForCanonicalityAudit(ctx, p.workerID,
		canonicalityAuditBatchSize, _confirmationLeaseTTL)
	if err != nil {
		return err
	}
	for _, payment := range payments {
		checkErr := p.auditSettledPayment(ctx, payment)
		if releaseErr := p.paymentRepo.ReleaseCanonicalityAuditClaim(ctx, payment.ID, p.workerID, checkErr); releaseErr != nil {
			checkErr = errors.Join(checkErr, releaseErr)
		}
		if checkErr != nil && ctx.Err() == nil {
			p.log.Errorw("settled payment canonicality check failed", "payment_id", payment.ID,
				"network_id", payment.NetworkID, "error", checkErr)
		}
	}
	// Individual failures have been logged and scheduled by the durable queue.
	// Only claim/read failures propagate to the loop's error logger.
	return nil
}

func (p *CryptoProcessor) auditSettledPayment(ctx context.Context, payment *crypto_payment.CryptoPayment) error {
	if p.drivers == nil {
		return fmt.Errorf("chain drivers unavailable")
	}
	if payment.BlockHash == "" || payment.BlockNumber < 0 {
		return fmt.Errorf("settled payment has no recorded block identity")
	}
	driver, err := p.drivers.Resolve(payment.NetworkID)
	if err != nil {
		return fmt.Errorf("resolve payment network: %w", err)
	}
	if verifier, ok := driver.(crypto.TransferVerifier); ok && verifier.VerificationRequired() {
		repo := crypto_repo.NewBlockIngestionRepo()
		expected, err := repo.ExpectedTransfer(ctx, payment)
		if err != nil {
			return err
		}
		proof, err := verifier.VerifyTransfer(ctx, expected)
		if err != nil {
			if errors.Is(err, crypto.ErrVerificationMismatch) {
				return errors.Join(err, repo.ReportIngestionFailure(ctx, payment.NetworkID, "canonicality", payment.BlockNumber, "transfer_mismatch", true))
			}
			return err
		}
		if proof.Event.Canonicality == crypto.CanonicalityOrphaned || proof.Event.BlockHash != payment.BlockHash || proof.Event.BlockNumber != payment.BlockNumber {
			if err := repo.ReportIngestionFailure(ctx, payment.NetworkID, "canonicality", payment.BlockNumber, "solidified_payment_changed", true); err != nil {
				return err
			}
			// Posted entries stay immutable. An absence proof permits the
			// existing reorg incident path, never an automatic ledger rewrite.
			if proof.Event.Canonicality == crypto.CanonicalityOrphaned {
				proofID, err := repo.SaveVerification(ctx, proof)
				if err != nil {
					return err
				}
				event := proof.Event
				event.VerificationID = &proofID
				return p.eventProcessor.ProcessEvent(ctx, event)
			}
			return crypto.ErrVerificationMismatch
		}
		return nil
	}
	canonical, ok := driver.(crypto.CanonicalBlockReader)
	if !ok {
		return fmt.Errorf("network driver does not support canonical block read")
	}
	canonicalHash, err := canonical.CanonicalBlock(ctx, payment.BlockNumber)
	if err != nil {
		return fmt.Errorf("read canonical block: %w", err)
	}
	if !strings.EqualFold(canonicalHash, payment.BlockHash) {
		event := crypto.BlockchainEvent{
			NetworkID: payment.NetworkID, TxHash: payment.TxHash,
			EventIndex: payment.EventIndex, EventKind: crypto.EventKind(payment.EventKind), Amount: payment.Amount,
			BlockNumber: payment.BlockNumber, BlockHash: canonicalHash, Finality: crypto.FinalityProvisional,
			Canonicality: crypto.CanonicalityOrphaned, Confirmations: 0, RequiredConfs: payment.RequiredConfs,
			EventType: crypto.EventTypeReorg,
		}
		if err := p.eventProcessor.ProcessEvent(ctx, event); err != nil {
			return fmt.Errorf("record settled payment reorg: %w", err)
		}
		return nil
	}
	receipt, err := driver.Finality().Receipt(ctx, payment.TxHash)
	if err != nil {
		return fmt.Errorf("read settled payment receipt: %w", err)
	}
	if receipt.Status != crypto.ReceiptSucceeded || receipt.Canonicality == crypto.CanonicalityOrphaned ||
		receipt.BlockHeight != payment.BlockNumber || !strings.EqualFold(receipt.BlockHash, payment.BlockHash) {
		return fmt.Errorf("settled payment receipt disagrees with recorded canonical block")
	}
	return nil
}
