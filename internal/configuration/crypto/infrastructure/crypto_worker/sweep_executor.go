package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/numerator"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

const _sweepExecPeriod = 1 * time.Minute

// SweepExecutor consolidates sweep_pending pool wallets to hot wallets.
type SweepExecutor struct {
	planner           *cryptoapp.AMLBatchSweepPlanner
	drivers           *crypto.ChainDriverRegistry
	signers           *crypto.FamilySignerRegistry
	sweepRepo         crypto_sweep.Repository
	outboundRepo      *OutboundRepo
	postingEngine     *posting.Engine
	numerator         numerator.Generator
	resourcePreparers ResourcePreparerResolver
	feeBudget         *FeeBudgetManager
	nativeFunding     *NativeFundingService
	authorizations    *SigningAuthorizationService
	tenantID          string
	log               *logger.Logger
}

// NewSweepExecutor creates the sweep executor.
func NewSweepExecutor(
	drivers *crypto.ChainDriverRegistry,
	signers *crypto.FamilySignerRegistry,
	sweepRepo crypto_sweep.Repository,
	outboundRepo *OutboundRepo,
	postingEngine *posting.Engine,
	num numerator.Generator,
	resourcePreparers ResourcePreparerResolver,
	feeBudget *FeeBudgetManager,
	authorizations *SigningAuthorizationService,
	tenantID string,
	log *logger.Logger,
) *SweepExecutor {
	if log == nil {
		log = logger.Default()
	}
	return &SweepExecutor{
		planner:           cryptoapp.NewAMLBatchSweepPlanner(crypto_repo.NewAMLRepository(), tenant.ContextTxManager{}),
		drivers:           drivers,
		signers:           signers,
		sweepRepo:         sweepRepo,
		outboundRepo:      outboundRepo,
		postingEngine:     postingEngine,
		numerator:         num,
		resourcePreparers: resourcePreparers,
		feeBudget:         feeBudget,
		nativeFunding:     NewNativeFundingService(outboundRepo, feeBudget, authorizations),
		authorizations:    authorizations,
		tenantID:          tenantID,
		log:               log.WithComponent("crypto_sweep_executor"),
	}
}

func (e *SweepExecutor) runLoop(ctx context.Context) {
	ticker := time.NewTicker(_sweepExecPeriod)
	defer ticker.Stop()

	e.log.Info("sweep executor started")
	for {
		select {
		case <-ctx.Done():
			e.log.Info("sweep executor stopped")
			return
		case <-ticker.C:
			if err := e.processBatch(ctx); err != nil {
				e.log.Errorw("sweep execution batch failed", "error", err)
			}
		}
	}
}

func (e *SweepExecutor) processBatch(ctx context.Context) error {
	for range _outboundBatchLimit {
		planned, err := e.planner.RunOnce(ctx)
		if err != nil {
			return fmt.Errorf("plan AML batch sweep: %w", err)
		}
		if !planned {
			break
		}
	}
	if err := e.outboundRepo.ExpireStaleSweepAttempts(ctx); err != nil {
		return err
	}

	tasks, err := e.outboundRepo.ClaimRunnableSweepAttempts(ctx, _outboundBatchLimit)
	if err != nil {
		return err
	}
	if len(tasks) < _outboundBatchLimit {
		newTasks, err := e.outboundRepo.ClaimSweepPendingWallets(ctx, _outboundBatchLimit-len(tasks))
		if err != nil {
			return err
		}
		tasks = append(tasks, newTasks...)
	}
	if len(tasks) == 0 {
		return nil
	}

	for _, group := range groupSweepTasks(tasks) {
		if err := e.processGroup(ctx, group); err != nil {
			if errors.Is(err, ErrNativeFundingPending) {
				e.log.Infow("sweep group waiting for native funding finality",
					"token_id", group[0].TokenID,
					"network_id", group[0].NetworkID,
					"count", len(group),
				)
				continue
			}
			e.log.Errorw("sweep group failed",
				"token_id", group[0].TokenID,
				"network_id", group[0].NetworkID,
				"count", len(group),
				"error", err,
			)
		}
	}
	return nil
}

func (e *SweepExecutor) application() *outboundapp.SweepExecutor {
	return outboundapp.NewSweepExecutor(outboundapp.SweepConfig{Drivers: e.drivers, Signers: e.signers, Documents: e.sweepRepo, Repository: e.outboundRepo, Posting: e.postingEngine, Numerator: e.numerator, Resources: e.resourcePreparers, Fees: feeBudgetPort(e.feeBudget), Funding: e.nativeFunding, Authorizations: authorizationPort(e.authorizations), TenantID: e.tenantID, Logger: e.log, Observer: executionObserver{}, Now: time.Now})
}
func (e *SweepExecutor) processGroup(ctx context.Context, group []SweepTask) error {
	return e.application().ExecuteGroup(ctx, group)
}
func groupSweepTasks(tasks []SweepTask) [][]SweepTask { return outboundapp.GroupSweepTasks(tasks) }
