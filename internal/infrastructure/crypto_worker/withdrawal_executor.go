package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_merchant_balance"
	"metapus/internal/infrastructure/storage/postgres/money_movement_repo"
	"metapus/pkg/logger"
)

const _withdrawalExecPeriod = 10 * time.Second

func isHeldExecutionError(err error) bool { return outboundapp.IsHeldExecutionError(err) }

// WithdrawalExecutor signs and broadcasts approved withdrawal requests.
type WithdrawalExecutor struct {
	drivers            *crypto.ChainDriverRegistry
	signers            *crypto.FamilySignerRegistry
	withdrawalRepo     crypto_withdrawal.Repository
	outboundRepo       *OutboundRepo
	approvalRepo       *money_movement_repo.Repo
	postingEngine      *posting.Engine
	numerator          numerator.Generator
	merchantBalanceSvc *crypto_merchant_balance.Service
	resourcePreparers  ResourcePreparerResolver
	feeBudget          *FeeBudgetManager
	nativeFunding      *NativeFundingService
	approvalSigners    crypto.MoneyMovementApprovalSignerProvider
	authorizations     *SigningAuthorizationService
	tenantID           string
	log                *logger.Logger
	now                func() time.Time
}

// NewWithdrawalExecutor creates the withdrawal executor.
func NewWithdrawalExecutor(
	drivers *crypto.ChainDriverRegistry,
	signers *crypto.FamilySignerRegistry,
	withdrawalRepo crypto_withdrawal.Repository,
	outboundRepo *OutboundRepo,
	approvalRepo *money_movement_repo.Repo,
	postingEngine *posting.Engine,
	num numerator.Generator,
	merchantBalanceSvc *crypto_merchant_balance.Service,
	resourcePreparers ResourcePreparerResolver,
	feeBudget *FeeBudgetManager,
	approvalSigners crypto.MoneyMovementApprovalSignerProvider,
	authorizations *SigningAuthorizationService,
	tenantID string,
	log *logger.Logger,
) *WithdrawalExecutor {
	if log == nil {
		log = logger.Default()
	}
	if merchantBalanceSvc == nil {
		panic("crypto_worker.NewWithdrawalExecutor: merchant balance service is required")
	}
	return &WithdrawalExecutor{
		drivers:            drivers,
		signers:            signers,
		withdrawalRepo:     withdrawalRepo,
		outboundRepo:       outboundRepo,
		approvalRepo:       approvalRepo,
		postingEngine:      postingEngine,
		numerator:          num,
		merchantBalanceSvc: merchantBalanceSvc,
		resourcePreparers:  resourcePreparers,
		feeBudget:          feeBudget,
		nativeFunding:      NewNativeFundingService(outboundRepo, feeBudget, authorizations),
		approvalSigners:    approvalSigners,
		authorizations:     authorizations,
		tenantID:           tenantID,
		log:                log.WithComponent("crypto_withdrawal_executor"),
	}
}

func (e *WithdrawalExecutor) runLoop(ctx context.Context) {
	ticker := time.NewTicker(_withdrawalExecPeriod)
	defer ticker.Stop()

	e.log.Info("withdrawal executor started")
	for {
		select {
		case <-ctx.Done():
			e.log.Info("withdrawal executor stopped")
			return
		case <-ticker.C:
			if err := e.processBatch(ctx); err != nil {
				e.log.Errorw("withdrawal execution batch failed", "error", err)
			}
		}
	}
}

func (e *WithdrawalExecutor) processBatch(ctx context.Context) error {
	defer e.observeRecoveryQueue(ctx)
	tasks, err := e.outboundRepo.ClaimApprovedWithdrawals(ctx, _outboundBatchLimit)
	if err != nil {
		return fmt.Errorf("claim withdrawals: %w", err)
	}
	riskTasks, err := e.outboundRepo.ClaimAMLWithdrawalLines(ctx, _outboundBatchLimit)
	if err != nil {
		return err
	}
	tasks = append(tasks, riskTasks...)
	if len(tasks) == 0 {
		return nil
	}

	for _, task := range tasks {
		if err := e.processOne(ctx, task); err != nil {
			if !id.IsNil(task.AMLParentID) {
				if recordErr := e.outboundRepo.DeferAMLWithdrawalLine(ctx, task, err); recordErr != nil {
					e.log.Errorw("AML withdrawal recovery hold failed", "error", recordErr)
				}
				continue
			}
			if errors.Is(err, ErrNativeFundingPending) {
				e.log.Infow("withdrawal waiting for native funding finality",
					"request_id", task.RequestID,
					"merchant_id", task.MerchantID,
					"token_id", task.TokenID,
				)
				continue
			}
			if errors.Is(err, ErrNoHotWalletLiquidity) {
				e.log.Warnw("withdrawal execution postponed: insufficient hot wallet liquidity",
					"request_id", task.RequestID,
					"merchant_id", task.MerchantID,
					"token_id", task.TokenID,
					"error", err,
				)
				if retryErr := e.outboundRepo.RequeueWithdrawalRequest(ctx, task.RequestID, statusFailedReason(err)); retryErr != nil {
					e.log.Errorw("failed to requeue withdrawal request",
						"request_id", task.RequestID,
						"error", retryErr,
					)
				}
				continue
			}

			e.log.Errorw("withdrawal execution failed",
				"request_id", task.RequestID,
				"merchant_id", task.MerchantID,
				"token_id", task.TokenID,
				"error", err,
			)
			if isHeldExecutionError(err) {
				if recordErr := e.outboundRepo.RecordWithdrawalRecovery(ctx, task.RequestID, withdrawalRecoveryReason(err), statusFailedReason(err)); recordErr != nil {
					e.log.Errorw("record withdrawal recovery hold", "request_id", task.RequestID, "error", recordErr)
				}
				e.log.Errorw("withdrawal execution requires manual reconciliation",
					"request_id", task.RequestID,
					"error", err,
				)
				continue
			}
			if !isPreEffectWithdrawalError(err) {
				// Database/RPC/signing failures never prove absence of a payment.
				// Keep the reservation and retry the existing request.
				continue
			}
			if failErr := e.failRequest(ctx, task, err); failErr != nil {
				e.log.Errorw("failed to mark withdrawal request failed",
					"request_id", task.RequestID,
					"error", failErr,
				)
			}
		}
	}
	return nil
}

func (e *WithdrawalExecutor) application() *outboundapp.WithdrawalExecutor {
	return outboundapp.NewWithdrawalExecutor(outboundapp.WithdrawalConfig{Drivers: e.drivers, Signers: e.signers, Documents: e.withdrawalRepo, Repository: e.outboundRepo, Approvals: approvalBundlePort(e.approvalRepo), ApprovalSigners: e.approvalSigners, Posting: e.postingEngine, Numerator: e.numerator, Resources: e.resourcePreparers, Fees: feeBudgetPort(e.feeBudget), Funding: e.nativeFunding, Authorizations: authorizationPort(e.authorizations), Transactions: tenant.ContextTxManager{}, TenantID: e.tenantID, Logger: e.log, Observer: executionObserver{}, Now: e.now})
}
func (e *WithdrawalExecutor) processOne(ctx context.Context, task WithdrawalTask) error {
	return e.application().Execute(ctx, task)
}
func (e *WithdrawalExecutor) failRequest(ctx context.Context, task WithdrawalTask, cause error) error {
	service := cryptoapp.NewWithdrawalFailureService(contextTxManager{}, e.outboundRepo, e.merchantBalanceSvc)
	changed, err := service.FailWithdrawalBeforeEffect(ctx, task.RequestID, statusFailedReason(cause))
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	e.log.Warnw("withdrawal request failed with storno",
		"request_id", task.RequestID,
		"status", withdrawal_request.StatusFailed,
		"error", cause,
	)
	return nil
}
