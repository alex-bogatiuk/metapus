package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_custody_receipt"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/numerator"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/posting"
)

const (
	_receiptIntakePeriod     = 15 * time.Second
	_receiptIntakeBatchLimit = 50
	_receiptIntakeLease      = 2 * time.Minute
	// A transfer without an allocation may still be matched by a late invoice
	// lease; afterwards it is accounted and only an operator credit can attribute it.
	_receiptUnallocatedGrace = 30 * time.Minute
)

// receiptIntake turns final unmatched incoming transfers into custody receipts
// and keeps auditing the canonicality of the accounted ones.
type receiptIntake struct {
	inbox   *crypto_repo.ChainEventInboxRepo
	proofs  *crypto_repo.BlockIngestionRepo
	audits  *crypto_repo.ReceiptAuditRepo
	service *cryptoapp.ReceiptIntakeService
	// paymentReorg compensates a posted invoice payment of an orphaned
	// receipt inside the audit transaction.
	paymentReorg receiptPaymentReorg
}

type receiptPaymentReorg func(ctx context.Context, paymentID id.ID, task crypto_repo.ReceiptAuditTask, event crypto.BlockchainEvent) error

func newReceiptIntake(engine *posting.Engine, numbers numerator.Generator) *receiptIntake {
	return &receiptIntake{
		inbox:  crypto_repo.NewChainEventInboxRepo(),
		proofs: crypto_repo.NewBlockIngestionRepo(),
		audits: crypto_repo.NewReceiptAuditRepo(),
		service: cryptoapp.NewReceiptIntakeService(postgres.NewContextTxManager(), crypto_repo.NewReceiptIntakeRepo(),
			crypto_repo.NewBlockIngestionRepo(), engine, numbers, postgres.NewOutboxPublisher(), time.Now),
	}
}

func (p *CryptoProcessor) runReceiptIntakeLoop(ctx context.Context) {
	if p.receipts == nil {
		return
	}
	ticker := time.NewTicker(_receiptIntakePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.drivers == nil {
				continue
			}
			if _, err := p.receipts.poll(ctx, p.drivers); err != nil {
				p.log.Errorw("custody receipt intake failed", "error", err)
			}
		}
	}
}

// poll returns the number of receipts recorded in this pass.
func (r *receiptIntake) poll(ctx context.Context, drivers *crypto.ChainDriverRegistry) (int, error) {
	var rows []crypto.InboundChainEvent
	err := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		claimed, err := r.inbox.ClaimReceiptCandidates(txCtx, _receiptIntakeBatchLimit, _receiptIntakeLease, _receiptUnallocatedGrace)
		rows = claimed
		return err
	})
	if err != nil {
		return 0, err
	}
	recorded := 0
	for i := range rows {
		ok, err := r.record(ctx, drivers, rows[i])
		if err != nil {
			if deferErr := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
				return r.inbox.DeferReceiptCandidate(txCtx, rows[i].ID, err.Error())
			}); deferErr != nil {
				return recorded, deferErr
			}
			continue
		}
		if ok {
			recorded++
		}
	}
	return recorded, nil
}

func (r *receiptIntake) record(ctx context.Context, drivers *crypto.ChainDriverRegistry, row crypto.InboundChainEvent) (bool, error) {
	if row.WalletID == nil || row.TokenID == nil {
		return false, fmt.Errorf("receipt candidate has no managed wallet or supported token")
	}
	reason := crypto_custody_receipt.Reason(row.MatchReason)
	if !reason.Valid() {
		return false, fmt.Errorf("inbox reason %q is not a receipt reason", row.MatchReason)
	}
	driver, err := drivers.Resolve(row.NetworkID)
	if err != nil {
		return false, err
	}
	verifier, ok := driver.(crypto.TransferVerifier)
	if !ok || !verifier.VerificationRequired() {
		return false, errors.New("network has no independent transfer proof")
	}
	proof, err := verifier.VerifyTransfer(ctx, expectedInboxTransfer(row))
	if err != nil {
		return false, err
	}
	if proof.Event.Finality != crypto.FinalitySolidified || proof.Event.Canonicality != crypto.CanonicalityCanonical {
		return false, crypto.ErrVerificationPending
	}
	proofID, err := r.proofs.SaveVerification(ctx, proof)
	if err != nil {
		return false, err
	}
	recorded := false
	err = (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		current, eligible, err := r.inbox.LockReceiptCandidate(txCtx, row.ID)
		if err != nil || !eligible {
			return err
		}
		event := proof.Event
		event.VerificationID = &proofID
		doc, err := r.service.Record(txCtx, cryptoapp.RecordCustodyReceiptCommand{
			Basis: crypto.AccountingTransferBasis{
				NetworkID: current.NetworkID, WalletID: *current.WalletID, TokenID: *current.TokenID,
				TxHash: current.TxHash, EventKind: crypto.EventKind(current.EventKind), EventIndex: current.EventIndex,
				FromAddress: current.FromAddress, RecordType: entity.RecordTypeReceipt, Amount: current.Amount,
				BlockNumber: event.BlockNumber, BlockHash: event.BlockHash, RequiredConfs: current.RequiredConfs,
				VerificationID: &proofID,
			},
			Event:  event,
			Reason: reason,
		})
		if err != nil {
			return err
		}
		recorded = true
		return r.inbox.MarkReceiptRecorded(txCtx, current.ID, doc.ID)
	})
	return recorded, err
}

func expectedInboxTransfer(row crypto.InboundChainEvent) crypto.BlockchainEvent {
	return crypto.BlockchainEvent{
		NetworkID: row.NetworkID, TxHash: row.TxHash, EventKind: crypto.EventKind(row.EventKind), EventIndex: row.EventIndex,
		FromAddress: row.FromAddress, ToAddress: row.ToAddress, TokenContract: row.TokenContract, Amount: row.Amount,
		BlockNumber: row.BlockNumber, BlockHash: row.BlockHash, RequiredConfs: row.RequiredConfs,
		EventType: crypto.EventTypeTransfer, Direction: crypto.TransferDirectionIncoming,
	}
}
