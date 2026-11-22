package outboundapp

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/outbound_settlement"
	"metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/logger"
	"metapus/pkg/sdk/tx"
)

type SettlementConfig struct {
	Drivers            *crypto.ChainDriverRegistry
	Repository         SettlementRepository
	Fees               SettlementFees
	Resources          ResourcePreparerResolver
	Transactions       tx.Manager
	Webhooks           crypto.LifecycleWebhookPublisher
	Observer           SettlementObserver
	Logger             *logger.Logger
	Now                func() time.Time
	SweepEvidence      SweepSettlementEvidence
	TreasuryEvidence   TreasurySettlementEvidence
	WithdrawalEvidence WithdrawalSettlementEvidence
}

type SettlementService struct {
	drivers            *crypto.ChainDriverRegistry
	outboundRepo       SettlementRepository
	feeBudget          SettlementFees
	resourcePreparers  ResourcePreparerResolver
	transactions       tx.Manager
	webhookPublisher   crypto.LifecycleWebhookPublisher
	observer           SettlementObserver
	log                *logger.Logger
	now                func() time.Time
	sweepEvidence      SweepSettlementEvidence
	treasuryEvidence   TreasurySettlementEvidence
	withdrawalEvidence WithdrawalSettlementEvidence
}

func NewSettlementService(c SettlementConfig) *SettlementService {
	if c.Logger == nil {
		c.Logger = logger.Default()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &SettlementService{
		drivers: c.Drivers, outboundRepo: c.Repository, feeBudget: c.Fees,
		resourcePreparers: c.Resources, transactions: c.Transactions, webhookPublisher: c.Webhooks,
		observer: c.Observer, log: c.Logger, now: c.Now,
		sweepEvidence:      c.SweepEvidence,
		treasuryEvidence:   c.TreasuryEvidence,
		withdrawalEvidence: c.WithdrawalEvidence,
	}
}

func (p *SettlementService) RecoverOrphanedResources(ctx context.Context, limit int) error {
	if p == nil || p.outboundRepo == nil || p.drivers == nil {
		return fmt.Errorf("outbound resource recovery is not configured")
	}
	tasks, err := p.outboundRepo.ListOrphanedResourceAllocations(ctx, limit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, task := range tasks {
		driver, resolveErr := p.drivers.Resolve(task.NetworkID)
		if resolveErr != nil {
			if firstErr == nil {
				firstErr = resolveErr
			}
			continue
		}
		preparer := resolveResourcePreparer(p.resourcePreparers, driver.Network().Family)
		if preparer == nil {
			continue
		}
		receipt, receiptErr := driver.Finality().Receipt(ctx, task.TxHash)
		if receiptErr != nil {
			// A signed authorization may be marked broadcast just before the RPC
			// indexes it. Leave the immutable reservation for the next poll.
			continue
		}
		if err := preparer.MarkResourceBroadcast(ctx, ResourceBroadcastCommand{
			AllocationID: task.AllocationID,
			BasisType:    task.BasisType,
			BasisID:      task.BasisID,
			TxHash:       task.TxHash,
		}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if receipt.FinanciallyFinal() {
			if err := preparer.SettleResource(ctx, task.TxHash, receipt); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (p *SettlementService) Settle(ctx context.Context, task SettlementTask) (err error) {
	if p == nil || p.outboundRepo == nil || p.drivers == nil || p.feeBudget == nil || p.transactions == nil {
		return fmt.Errorf("outbound settlement service is not configured")
	}
	_, _, purpose, _ := settlementFeeBasis(task)
	if p.observer != nil {
		var finish func(error)
		ctx, finish = p.observer.Start(ctx, "crypto.outbound.settle", task.NetworkID, purpose)
		defer func() { finish(err) }()
	}

	driver, err := p.drivers.Resolve(task.NetworkID)
	if err != nil {
		_ = p.outboundRepo.HoldOutboundSettlementClaim(ctx, task, err)
		return err
	}
	finalityCtx := ctx
	finishFinality := func(error) {}
	if p.observer != nil {
		finalityCtx, finishFinality = p.observer.Finality(ctx, driver.Network())
	}
	receipt, err := driver.Finality().Receipt(finalityCtx, task.TxHash)
	finishFinality(err)
	if err != nil {
		_ = p.outboundRepo.HoldOutboundSettlementClaim(ctx, task, err)
		return err
	}
	if receipt.Status == crypto.ReceiptPending || !receipt.FinanciallyFinal() {
		return p.outboundRepo.ReleaseOutboundSettlementClaim(ctx, task)
	}
	if receipt.ObservedAt.IsZero() {
		receipt.ObservedAt = p.now().UTC()
	}
	if task.Kind == SettlementSweep && receipt.Status == crypto.ReceiptSucceeded {
		if p.sweepEvidence == nil {
			_ = p.outboundRepo.ReleaseOutboundSettlementClaim(ctx, task)
			return crypto.ErrVerificationPending
		}
		proofID, proofErr := p.sweepEvidence.VerifySweep(ctx, driver, task, receipt)
		if proofErr != nil {
			_ = p.outboundRepo.ReleaseOutboundSettlementClaim(ctx, task)
			return proofErr
		}
		task.VerificationID = &proofID
	}
	if task.Kind == SettlementTreasury && receipt.Status == crypto.ReceiptSucceeded {
		if p.treasuryEvidence == nil {
			_ = p.outboundRepo.ReleaseOutboundSettlementClaim(ctx, task)
			return crypto.ErrVerificationPending
		}
		proofID, proofErr := p.treasuryEvidence.VerifyTreasury(ctx, driver, task, receipt)
		if proofErr != nil {
			_ = p.outboundRepo.ReleaseOutboundSettlementClaim(ctx, task)
			return proofErr
		}
		task.VerificationID = &proofID
	}
	result := outbound_settlement.ChainResultSuccess
	if task.Kind == SettlementWithdrawal && receipt.Status == crypto.ReceiptSucceeded {
		if p.withdrawalEvidence == nil {
			_ = p.outboundRepo.ReleaseOutboundSettlementClaim(ctx, task)
			return crypto.ErrVerificationPending
		}
		proofID, proofErr := p.withdrawalEvidence.VerifyWithdrawal(ctx, driver, task, receipt)
		if proofErr != nil {
			_ = p.outboundRepo.ReleaseOutboundSettlementClaim(ctx, task)
			return proofErr
		}
		task.VerificationID = &proofID
	}
	if receipt.Status == crypto.ReceiptReverted {
		result = outbound_settlement.ChainResult("REVERTED")
	}
	basisType, basisID, purpose, err := settlementFeeBasis(task)
	if err != nil {
		return err
	}
	reservation, found, err := p.feeBudget.Get(ctx, basisType, basisID, purpose, 1)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("outbound fee reservation is missing")
	}
	// A cap violation is an incident and must commit its hold independently.
	// Ordinary settlement, however, belongs to the same transaction as the
	// business document and ledger effects below.
	if receipt.Fee.TotalFee.Cmp(reservation.MaxFee) > 0 {
		return p.feeBudget.Settle(ctx, reservation.ID, receipt)
	}
	err = p.transactions.RunInTransaction(ctx, func(txCtx context.Context) error {
		proceed, err := p.admitSettlement(txCtx, task, receipt)
		if err != nil {
			return err
		}
		if !proceed {
			return p.feeBudget.Settle(txCtx, reservation.ID, receipt)
		}
		if err := p.feeBudget.Settle(txCtx, reservation.ID, receipt); err != nil {
			return err
		}
		if err := p.outboundRepo.RecordOutboundSettlement(txCtx, task, receipt); err != nil {
			return err
		}
		if preparer := resolveResourcePreparer(p.resourcePreparers, driver.Network().Family); preparer != nil {
			if err := preparer.SettleResource(txCtx, task.TxHash, receipt); err != nil {
				return fmt.Errorf("settle chain resource allocation: %w", err)
			}
		}
		if task.Kind == SettlementWithdrawal && id.IsNil(task.LineID) && p.webhookPublisher != nil {
			eventType := crypto.WebhookWithdrawalConfirmed
			if result != outbound_settlement.ChainResultSuccess {
				eventType = crypto.WebhookWithdrawalFailed
			}
			if err := p.webhookPublisher.PublishLifecycleWebhook(txCtx, crypto.LifecycleWebhookEvent{
				Event:           eventType,
				AggregateID:     task.RequestID,
				MerchantID:      task.MerchantID,
				ResourceVersion: max(task.PostedVersion, 1),
				OccurredAt:      p.now().UTC(),
				Data: map[string]any{
					"id":                  task.RequestID.String(),
					"withdrawalRequestId": task.RequestID.String(),
					"withdrawalId":        task.DocumentID.String(),
					"merchantId":          task.MerchantID.String(),
					"tokenId":             task.OperationTokenID.String(),
					"amount":              task.Amount,
					"txHash":              task.TxHash,
					"chainResult":         result,
				},
			}); err != nil {
				return fmt.Errorf("publish withdrawal lifecycle webhook: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		_ = p.outboundRepo.HoldOutboundSettlementClaim(ctx, task, err)
		return err
	}
	p.log.Infow("outbound transaction settled",
		"kind", task.Kind, "document_id", task.DocumentID, "line_id", task.LineID,
		"tx_hash", task.TxHash, "result", result, "network_fee", receipt.Fee.TotalFee)
	return nil
}

func settlementFeeBasis(task SettlementTask) (string, id.ID, crypto.SigningPurpose, error) {
	switch task.Kind {
	case SettlementWithdrawal:
		if !id.IsNil(task.LineID) {
			return "CryptoAMLWithdrawalLine", task.LineID, crypto.SigningPurposeWithdrawal, nil
		}
		return withdrawal_request.DocumentType, task.RequestID, crypto.SigningPurposeWithdrawal, nil
	case SettlementSweep:
		return _sweepAttemptBasisType, task.LineID, crypto.SigningPurposeSweep, nil
	case SettlementTreasury:
		return treasury_withdrawal.DocumentType, task.DocumentID, crypto.SigningPurposeTreasury, nil
	default:
		return "", id.ID{}, "", fmt.Errorf("unknown outbound settlement kind %q", task.Kind)
	}
}

// admitSettlement runs the document-specific financial admission inside the
// settlement transaction.
func (p *SettlementService) admitSettlement(ctx context.Context, task SettlementTask, receipt crypto.ChainReceipt) (bool, error) {
	switch task.Kind {
	case SettlementWithdrawal:
		return p.outboundRepo.PrepareWithdrawalSettlement(ctx, task, receipt)
	case SettlementTreasury:
		return p.outboundRepo.PrepareTreasurySettlement(ctx, task, receipt)
	case SettlementSweep:
		return p.outboundRepo.PrepareSweepSettlement(ctx, task, receipt)
	}
	return false, fmt.Errorf("unsupported settlement kind %q", task.Kind)
}
