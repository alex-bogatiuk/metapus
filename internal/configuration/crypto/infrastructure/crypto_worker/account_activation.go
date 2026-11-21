package crypto_worker

import (
	"context"
	"sync"
	"time"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/workerjob"
	"metapus/pkg/logger"
)

const _accountActivationPeriod = outboundapp.ActivationRetryPeriod

func isChainDependencyError(err error) bool { return outboundapp.IsChainDependencyError(err) }

type AccountActivationTask = outbound_repo.AccountActivationTask

type activationCostRepairTask = outboundapp.ActivationCostRepairTask

// AccountActivationExecutor pre-activates accounts for drivers that expose the optional lifecycle capability.
type AccountActivationExecutor struct {
	drivers        *crypto.ChainDriverRegistry
	signers        *crypto.FamilySignerRegistry
	outboundRepo   *OutboundRepo
	authorizations *SigningAuthorizationService
	feeBudget      *FeeBudgetManager
	operations     *CryptoNetworkOperationCoordinator
	walletPoolRepo crypto.WalletPoolActivationStatusRepository
	tenantID       string
	workerID       string
	recorder       *workerjob.Recorder
	log            *logger.Logger
	gateMu         sync.Mutex
	networkGates   map[id.ID]*dependencyCircuit
}

func NewAccountActivationExecutor(
	drivers *crypto.ChainDriverRegistry,
	signers *crypto.FamilySignerRegistry,
	outboundRepo *OutboundRepo,
	authorizations *SigningAuthorizationService,
	feeBudget *FeeBudgetManager,
	operations *CryptoNetworkOperationCoordinator,
	walletPoolRepo crypto.WalletPoolActivationStatusRepository,
	tenantID string,
	recorder *workerjob.Recorder,
	log *logger.Logger,
) *AccountActivationExecutor {
	if log == nil {
		log = logger.Default()
	}
	return &AccountActivationExecutor{
		drivers:        drivers,
		signers:        signers,
		outboundRepo:   outboundRepo,
		authorizations: authorizations,
		feeBudget:      feeBudget,
		operations:     operations,
		walletPoolRepo: walletPoolRepo,
		tenantID:       tenantID,
		workerID:       tenantID + ":" + id.New().String(),
		recorder:       recorder,
		log:            log.WithComponent("crypto_account_activation"),
		networkGates:   make(map[id.ID]*dependencyCircuit),
	}
}

func (e *AccountActivationExecutor) runLoop(ctx context.Context) {
	ticker := time.NewTicker(_accountActivationPeriod)
	defer ticker.Stop()

	e.log.Info("chain account activation executor started")
	e.runRecordedBatch(ctx)
	for {
		select {
		case <-ctx.Done():
			e.log.Info("chain account activation executor stopped")
			return
		case <-ticker.C:
			e.runRecordedBatch(ctx)
		}
	}
}

func (e *AccountActivationExecutor) runRecordedBatch(ctx context.Context) {
	run := func(ctx context.Context) (int, error) {
		return e.runBatch(ctx)
	}
	if e.recorder != nil {
		e.recorder.RecordIfWork(ctx, "crypto.wallet_pool.activation", "crypto", run)
		return
	}
	if _, err := run(ctx); err != nil {
		e.log.Errorw("chain account activation batch failed", "error", err)
	}
}

func (e *AccountActivationExecutor) runBatch(ctx context.Context) (int, error) {
	processed := 0
	var firstErr error
	touchedNetworks := make(map[id.ID]struct{})
	repairs, err := e.outboundRepo.ClaimActivationCostRepairs(ctx, e.workerID, _outboundBatchLimit)
	if err != nil {
		return processed, err
	}
	for _, repair := range repairs {
		gate := e.networkGate(repair.NetworkID)
		if !gate.allow(time.Now().UTC()) {
			continue
		}
		if err := e.repairActivationCost(ctx, repair); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			e.log.Warnw("chain activation cost repair failed",
				"activation_id", repair.ActivationID,
				"tx_hash", repair.TxHash,
				"error", err,
			)
			if isChainDependencyError(err) {
				gate.failure(time.Now().UTC())
			} else {
				gate.cancelProbe()
			}
		} else {
			gate.success()
		}
		processed++
	}

	tasks, err := e.outboundRepo.ClaimAccountActivationTasks(
		ctx,
		e.workerID,
		_outboundBatchLimit,
	)
	if err != nil {
		return processed, err
	}
	for _, task := range tasks {
		gate := e.networkGate(task.NetworkID)
		if !gate.allow(time.Now().UTC()) {
			continue
		}
		touchedNetworks[task.NetworkID] = struct{}{}
		if err := e.processTask(ctx, task); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			e.log.Warnw("chain account activation task failed",
				"activation_id", task.ActivationID,
				"wallet_id", task.WalletID,
				"address", task.Address,
				"error", err,
			)
			if isChainDependencyError(err) {
				gate.failure(time.Now().UTC())
			} else {
				gate.cancelProbe()
			}
		} else {
			gate.success()
		}
		processed++
	}
	for networkID := range touchedNetworks {
		e.clearResolvedPoolError(ctx, networkID)
	}
	return processed, firstErr
}

func (e *AccountActivationExecutor) networkGate(networkID id.ID) *dependencyCircuit {
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	gate := e.networkGates[networkID]
	if gate == nil {
		gate = &dependencyCircuit{}
		e.networkGates[networkID] = gate
	}
	return gate
}

func (e *AccountActivationExecutor) clearResolvedPoolError(ctx context.Context, networkID id.ID) {
	if e.walletPoolRepo == nil {
		return
	}
	state, err := e.walletPoolRepo.GetState(ctx, networkID)
	if err != nil {
		e.log.Warnw("failed to inspect wallet pool after activation",
			"network_id", networkID,
			"error", err,
		)
		return
	}
	if state.Stats.Held != 0 || state.Stats.Failed != 0 || state.Stats.Provisioning != 0 {
		return
	}
	if err := e.walletPoolRepo.SetOperationalStatus(ctx, networkID, crypto.WalletPoolHealthyStatus()); err != nil {
		e.log.Warnw("failed to clear resolved wallet pool error",
			"network_id", networkID,
			"error", err,
		)
	}
}

func (e *AccountActivationExecutor) application() *outboundapp.AccountActivationExecutor {
	var fees outboundapp.ActivationFees
	if e.feeBudget != nil {
		fees = e.feeBudget
	}
	var operations outboundapp.ActivationOperations
	if e.operations != nil {
		operations = e.operations
	}
	return outboundapp.NewAccountActivationExecutor(outboundapp.ActivationConfig{Drivers: e.drivers, Signers: e.signers, Repository: e.outboundRepo, Authorizations: authorizationPort(e.authorizations), Fees: fees, Operations: operations, WalletPool: e.walletPoolRepo, Transactions: tenant.ContextTxManager{}, Logger: e.log, Observer: executionObserver{}, Now: time.Now})
}
func (e *AccountActivationExecutor) processTask(ctx context.Context, task AccountActivationTask) error {
	return e.application().Execute(ctx, task)
}
func (e *AccountActivationExecutor) repairActivationCost(ctx context.Context, task activationCostRepairTask) error {
	return e.application().RepairCost(ctx, task)
}
func expiredUnbroadcastSigningAttempt(record SigningAuthorizationRecord, now time.Time) bool {
	return outboundapp.ExpiredUnbroadcastSigningAttempt(record, now)
}
