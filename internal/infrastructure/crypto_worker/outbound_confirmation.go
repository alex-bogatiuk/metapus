package crypto_worker

import (
	"context"
	"time"

	"metapus/internal/application/outboundapp"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

const _outboundConfirmationPollPeriod = 30 * time.Second

func (p *CryptoProcessor) runOutboundConfirmationLoop(ctx context.Context) {
	ticker := time.NewTicker(_outboundConfirmationPollPeriod)
	defer ticker.Stop()

	p.log.Info("outbound confirmation and settlement loop started")
	for {
		select {
		case <-ctx.Done():
			p.log.Info("outbound confirmation and settlement loop stopped")
			return
		case <-ticker.C:
			if err := p.pollOutboundConfirmations(ctx); err != nil {
				p.log.Errorw("failed to settle outbound transactions", "error", err)
			}
		}
	}
}

func (p *CryptoProcessor) pollOutboundConfirmations(ctx context.Context) error {
	if p.drivers == nil || p.feeBudget == nil {
		return nil
	}
	if _, err := crypto_repo.NewExpectedTransferRepo().NudgePending(ctx, _outboundBatchLimit); err != nil {
		return err
	}
	var replacements outboundapp.ReplacementExecutor
	if p.outboundRepo != nil && p.authorizations != nil && p.familySigners != nil {
		replacements = outboundapp.NewReplacementService(outboundapp.ReplacementConfig{
			Drivers: p.drivers, Signers: p.familySigners, Repository: p.outboundRepo,
			Authorizations: p.authorizations, Fees: p.feeBudget, Transactions: contextTxManager{},
			Observer: executionObserver{}, Logger: p.log,
		})
	}
	return outboundapp.NewConfirmationCycle(outboundapp.ConfirmationCycleConfig{
		Claims: p.outboundRepo, Funding: p.nativeFundingConfirmationApplication(), Replacements: replacements,
		Settlement: p.settlementApplication(), Resources: p.resourcePreparers, Logger: p.log,
	}).RunBatch(ctx, p.workerID, _outboundBatchLimit)
}

func (p *CryptoProcessor) settlementApplication() *outboundapp.SettlementService {
	var repository outboundapp.SettlementRepository
	if p.outboundRepo != nil {
		repository = p.outboundRepo
	}
	var fees outboundapp.SettlementFees
	if p.feeBudget != nil {
		fees = p.feeBudget
	}
	return outboundapp.NewSettlementService(outboundapp.SettlementConfig{
		Drivers: p.drivers, Repository: repository, Fees: fees, Resources: p.resourcePreparers,
		Transactions: contextTxManager{}, Webhooks: p.webhookPublisher, Observer: executionObserver{}, Logger: p.log,
		SweepEvidence:      outboundapp.NewSweepEvidenceService(crypto_repo.NewSweepCustodyRepo()),
		TreasuryEvidence:   outboundapp.NewTreasuryEvidenceService(crypto_repo.NewTreasuryCustodyRepo()),
		WithdrawalEvidence: outboundapp.NewWithdrawalEvidenceService(crypto_repo.NewWithdrawalCustodyRepo()),
	})
}
