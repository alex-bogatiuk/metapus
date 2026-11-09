package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"metapus/internal/application/outboundapp"
	"time"

	"metapus/internal/core/numerator"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/storage/postgres/money_movement_repo"
	"metapus/pkg/logger"
)

const _treasuryExecPeriod = 10 * time.Second

// TreasuryWithdrawalExecutor signs and broadcasts approved platform treasury withdrawals.
type TreasuryWithdrawalExecutor struct {
	outboundRepo *OutboundRepo
	useCase      *outboundapp.TreasuryExecutor
	log          *logger.Logger
}

// NewTreasuryWithdrawalExecutor creates the treasury withdrawal executor.
func NewTreasuryWithdrawalExecutor(
	drivers *crypto.ChainDriverRegistry,
	signers *crypto.FamilySignerRegistry,
	treasuryRepo treasury_withdrawal.Repository,
	outboundRepo *OutboundRepo,
	approvalRepo *money_movement_repo.Repo,
	postingEngine *posting.Engine,
	num numerator.Generator,
	resourcePreparers ResourcePreparerResolver,
	feeBudget *FeeBudgetManager,
	approvalSigners crypto.MoneyMovementApprovalSignerProvider,
	authorizations *SigningAuthorizationService,
	tenantID string,
	log *logger.Logger,
) *TreasuryWithdrawalExecutor {
	if log == nil {
		log = logger.Default()
	}
	return &TreasuryWithdrawalExecutor{
		outboundRepo: outboundRepo, log: log.WithComponent("crypto_treasury_executor"),
		useCase: outboundapp.NewTreasuryExecutor(drivers, signers, treasuryRepo, outboundRepo, approvalBundlePort(approvalRepo), postingEngine, num, resourcePreparers, feeBudgetPort(feeBudget), approvalSigners, authorizationPort(authorizations), tenantID, log, executionObserver{}, time.Now),
	}
}

func (e *TreasuryWithdrawalExecutor) runLoop(ctx context.Context) {
	ticker := time.NewTicker(_treasuryExecPeriod)
	defer ticker.Stop()

	e.log.Info("treasury withdrawal executor started")
	for {
		select {
		case <-ctx.Done():
			e.log.Info("treasury withdrawal executor stopped")
			return
		case <-ticker.C:
			if err := e.processBatch(ctx); err != nil {
				e.log.Errorw("treasury withdrawal execution batch failed", "error", err)
			}
		}
	}
}

func (e *TreasuryWithdrawalExecutor) processBatch(ctx context.Context) error {
	tasks, err := e.outboundRepo.ClaimApprovedTreasuryWithdrawals(ctx, _outboundBatchLimit)
	if err != nil {
		return fmt.Errorf("claim treasury withdrawals: %w", err)
	}
	for _, task := range tasks {
		if err := e.useCase.Execute(ctx, task); err != nil {
			if errors.Is(err, ErrNoHotWalletLiquidity) {
				e.log.Warnw("treasury withdrawal postponed: insufficient hot wallet liquidity",
					"withdrawal_id", task.WithdrawalID,
					"token_id", task.TokenID,
					"error", err,
				)
				if retryErr := e.outboundRepo.RequeueTreasuryWithdrawal(ctx, task.WithdrawalID, statusFailedReason(err)); retryErr != nil {
					e.log.Errorw("failed to requeue treasury withdrawal", "withdrawal_id", task.WithdrawalID, "error", retryErr)
				}
				continue
			}

			e.log.Errorw("treasury withdrawal execution failed",
				"withdrawal_id", task.WithdrawalID,
				"token_id", task.TokenID,
				"error", err,
			)
			// Retain reservations until a final settlement or an explicit
			// pre-effect application operation establishes a safe outcome.

		}
	}
	return nil
}
