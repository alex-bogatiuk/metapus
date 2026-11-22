package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/logger"
	"metapus/pkg/sdk/workerjob"
)

const (
	walletPoolControllerPeriod    = 15 * time.Second
	walletProvisioningLease       = 2 * time.Minute
	walletProvisioningClaimLimit  = dependencyCircuitThreshold
	walletProvisioningMaxAttempts = 10
	dependencyCircuitThreshold    = 5
	dependencyCircuitOpenFor      = 30 * time.Second
)

type walletReconciler func(context.Context, id.ID) (ReconciliationResult, error)

// WalletPoolController keeps ready receiving capacity above its configured
// watermark. External Vault/TRON calls are made only by this asynchronous path.
type WalletPoolController struct {
	repo       crypto.WalletPoolControllerRepository
	drivers    *crypto.ChainDriverRegistry
	reconcile  walletReconciler
	workerID   string
	recorder   *workerjob.Recorder
	log        *logger.Logger
	vaultGate  dependencyCircuit
	gateMu     sync.Mutex
	chainGates map[id.ID]*dependencyCircuit
}

func NewWalletPoolController(
	repo crypto.WalletPoolControllerRepository,
	drivers *crypto.ChainDriverRegistry,
	reconcile walletReconciler,
	workerID string,
	recorder *workerjob.Recorder,
	log *logger.Logger,
) *WalletPoolController {
	if log == nil {
		log = logger.Default()
	}
	return &WalletPoolController{
		repo:       repo,
		drivers:    drivers,
		reconcile:  reconcile,
		workerID:   workerID,
		recorder:   recorder,
		log:        log.WithComponent("wallet_pool_controller"),
		chainGates: make(map[id.ID]*dependencyCircuit),
	}
}

func (c *WalletPoolController) runLoop(ctx context.Context) {
	c.log.Info("wallet pool controller started")
	c.runCycle(ctx)

	timer := time.NewTimer(walletPoolNextDelay())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			c.log.Info("wallet pool controller stopped")
			return
		case <-timer.C:
			c.runCycle(ctx)
			timer.Reset(walletPoolNextDelay())
		}
	}
}

func walletPoolNextDelay() time.Duration {
	// Stable bounded jitter avoids synchronized controllers without relying on
	// a mutable global PRNG. Range is 12–18 seconds.
	jitterWindow := 6 * time.Second
	offset := time.Duration(time.Now().UnixNano()%int64(jitterWindow)) - jitterWindow/2
	return walletPoolControllerPeriod + offset
}

func (c *WalletPoolController) runCycle(ctx context.Context) {
	runReconcile := func(ctx context.Context) (int, error) {
		return c.reconcileCapacity(ctx)
	}
	if c.recorder != nil {
		c.recorder.RecordIfWork(ctx, "crypto.wallet_pool.reconcile", "crypto", runReconcile)
	} else if _, err := runReconcile(ctx); err != nil {
		c.log.Errorw("wallet pool capacity reconciliation failed", "error", err)
	}

	runProvision := func(ctx context.Context) (int, error) {
		return c.provision(ctx)
	}
	if c.recorder != nil {
		c.recorder.RecordIfWork(ctx, "crypto.wallet_pool.provision", "crypto", runProvision)
	} else if _, err := runProvision(ctx); err != nil {
		c.log.Errorw("wallet provisioning batch failed", "error", err)
	}
}

func (c *WalletPoolController) reconcileCapacity(ctx context.Context) (int, error) {
	networkIDs, err := c.repo.ListRefillCandidates(ctx)
	if err != nil {
		return 0, err
	}
	created := 0
	var planErrors []error
	for _, networkID := range networkIDs {
		driver, resolveErr := c.drivers.Resolve(networkID)
		if resolveErr != nil {
			_ = c.repo.SetOperationalStatus(ctx, networkID, crypto.WalletPoolOperationalStatus{
				ErrorCode:     crypto.WalletPoolErrorDriverUnavailable,
				PublicMessage: "Runtime-адаптер сети не готов. Автопополнение продолжится после восстановления конфигурации.",
			})
			planErrors = append(planErrors, fmt.Errorf("resolve wallet provisioning driver for network %s: %w", networkID, resolveErr))
			continue
		}
		if !driver.Capabilities().WalletProvisioning.Supported() {
			_ = c.repo.SetOperationalStatus(ctx, networkID, crypto.WalletPoolOperationalStatus{
				ErrorCode:     crypto.WalletPoolErrorProvisioningUnsupported,
				PublicMessage: "Адаптер сети не поддерживает автоматическое создание адресов.",
			})
			continue
		}
		state, stateErr := c.repo.GetState(ctx, networkID)
		if stateErr != nil {
			planErrors = append(planErrors, fmt.Errorf("load wallet pool policy for network %s: %w", networkID, stateErr))
			continue
		}
		if policyErr := state.Policy.Validate(driver.Capabilities()); policyErr != nil {
			_ = c.repo.SetOperationalStatus(ctx, networkID, crypto.WalletPoolOperationalStatus{
				ErrorCode:     crypto.WalletPoolErrorPolicyInvalid,
				PublicMessage: "Политика пула не соответствует capabilities адаптера сети.",
			})
			planErrors = append(planErrors, fmt.Errorf("validate wallet pool policy for network %s: %w", networkID, policyErr))
			continue
		}
		switch state.Policy.LastErrorCode {
		case crypto.WalletPoolErrorDriverUnavailable,
			crypto.WalletPoolErrorProvisioningUnsupported,
			crypto.WalletPoolErrorPolicyInvalid:
			_ = c.repo.SetOperationalStatus(ctx, networkID, crypto.WalletPoolHealthyStatus())
		}
		result, planErr := c.repo.PlanNetworkRefill(ctx, networkID)
		if planErr != nil {
			planErrors = append(planErrors, fmt.Errorf("plan wallet refill for network %s: %w", networkID, planErr))
			continue
		}
		created += result.Created
		if result.Created > 0 {
			c.log.Infow("wallet refill jobs planned",
				"network_id", result.NetworkID,
				"ready", result.Ready,
				"in_flight", result.InFlight,
				"total", result.Total,
				"created", result.Created,
			)
		}
	}

	states, err := c.repo.ListStates(ctx)
	if err != nil {
		return created, err
	}
	for _, state := range states {
		if err := c.repo.RecordStateTransition(ctx, state); err != nil {
			c.log.Warnw("failed to record wallet pool state transition",
				"network_id", state.Policy.NetworkID,
				"state", state.State,
				"error", err,
			)
		}
	}
	return created, errors.Join(planErrors...)
}

func (c *WalletPoolController) provision(ctx context.Context) (int, error) {
	completed := 0
	if c.vaultGate.allow(time.Now().UTC()) {
		jobs, err := c.repo.ClaimProvisioningJobs(
			ctx,
			c.workerID,
			walletProvisioningClaimLimit,
			walletProvisioningLease,
		)
		if err != nil {
			return 0, err
		}
		if len(jobs) == 0 {
			c.vaultGate.cancelProbe()
		}

		for _, job := range jobs {
			if ctx.Err() != nil {
				return completed, ctx.Err()
			}
			driver, deriveErr := c.drivers.Resolve(job.NetworkID)
			if deriveErr != nil {
				_ = c.repo.RetryProvisioningJob(ctx, job, deriveErr, time.Now().UTC(), true)
				_ = c.repo.SetOperationalStatus(ctx, job.NetworkID, crypto.WalletPoolOperationalStatus{
					ErrorCode:     crypto.WalletPoolErrorDriverUnavailable,
					PublicMessage: "Runtime-адаптер сети не готов. Задание удерживается до восстановления конфигурации.",
				})
				continue
			}
			if !driver.Capabilities().WalletProvisioning.Supported() {
				deriveErr = fmt.Errorf("family %s does not support wallet provisioning", driver.Network().Family)
				_ = c.repo.RetryProvisioningJob(ctx, job, deriveErr, time.Now().UTC(), true)
				_ = c.repo.SetOperationalStatus(ctx, job.NetworkID, crypto.WalletPoolOperationalStatus{
					ErrorCode:     crypto.WalletPoolErrorProvisioningUnsupported,
					PublicMessage: "Адаптер сети не поддерживает автоматическое создание адресов.",
				})
				continue
			}
			state, stateErr := c.repo.GetState(ctx, job.NetworkID)
			if stateErr != nil {
				_ = c.repo.RetryProvisioningJob(ctx, job, stateErr, time.Now().UTC().Add(walletPoolControllerPeriod), false)
				continue
			}
			if !state.Policy.Enabled || state.Policy.NeedsReview {
				policyErr := errors.New("wallet pool policy is disabled or needs review")
				_ = c.repo.RetryProvisioningJob(ctx, job, policyErr, time.Now().UTC().Add(walletPoolControllerPeriod), false)
				continue
			}
			if policyErr := state.Policy.Validate(driver.Capabilities()); policyErr != nil {
				_ = c.repo.RetryProvisioningJob(ctx, job, policyErr, time.Now().UTC(), true)
				_ = c.repo.SetOperationalStatus(ctx, job.NetworkID, crypto.WalletPoolOperationalStatus{
					ErrorCode:     crypto.WalletPoolErrorPolicyInvalid,
					PublicMessage: "Политика пула не соответствует capabilities адаптера сети. Задание удерживается.",
				})
				continue
			}
			var derivation crypto.WalletDerivation
			derivation, deriveErr = driver.Wallets().DeriveAddress(ctx, driver.Network(), job.DerivationPath)
			if deriveErr != nil {
				c.vaultGate.failure(time.Now().UTC())
				held := job.Attempts >= walletProvisioningMaxAttempts
				next := time.Now().UTC().Add(provisioningBackoff(job.Attempts))
				if retryErr := c.repo.RetryProvisioningJob(ctx, job, deriveErr, next, held); retryErr != nil {
					return completed, fmt.Errorf("persist Vault derive failure: %w", retryErr)
				}
				_ = c.repo.SetOperationalStatus(ctx, job.NetworkID, crypto.WalletPoolOperationalStatus{
					ErrorCode:     crypto.WalletPoolErrorVaultUnavailable,
					PublicMessage: "Vault временно недоступен. Автопополнение продолжится автоматически.",
				})
				c.log.Warnw("Vault wallet derivation failed",
					"job_id", job.ID,
					"network_id", job.NetworkID,
					"attempt", job.Attempts,
					"held", held,
					"error", deriveErr,
				)
				if !c.vaultGate.allow(time.Now().UTC()) {
					break
				}
				continue
			}
			if err := crypto.ValidateWalletDerivation(job, derivation); err != nil {
				c.vaultGate.failure(time.Now().UTC())
				_ = c.repo.RetryProvisioningJob(ctx, job, err, time.Now().UTC(), true)
				_ = c.repo.SetOperationalStatus(ctx, job.NetworkID, crypto.WalletPoolOperationalStatus{
					ErrorCode:     crypto.WalletPoolErrorVaultInvalidResponse,
					PublicMessage: "Vault вернул некорректный derivation result. Автопополнение удерживается.",
				})
				if !c.vaultGate.allow(time.Now().UTC()) {
					break
				}
				continue
			}
			c.vaultGate.success()

			if _, err := c.repo.CompleteProvisioningJob(ctx, job, derivation, driver.Capabilities().WalletProvisioning); err != nil {
				next := time.Now().UTC().Add(provisioningBackoff(job.Attempts))
				_ = c.repo.RetryProvisioningJob(ctx, job, err, next, job.Attempts >= walletProvisioningMaxAttempts)
				return completed, err
			}
			completed++
		}
	}

	reconciled, err := c.reconcilePendingWallets(ctx)
	return completed + reconciled, err
}

func (c *WalletPoolController) reconcilePendingWallets(ctx context.Context) (int, error) {
	if c.reconcile == nil {
		return 0, nil
	}
	items, err := c.repo.ListPendingWalletReconciliations(ctx, walletProvisioningClaimLimit)
	if err != nil {
		return 0, err
	}

	reconciled := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return reconciled, ctx.Err()
		}
		gate := c.chainGate(item.NetworkID)
		if !gate.allow(time.Now().UTC()) {
			continue
		}
		result, reconcileErr := c.reconcile(ctx, item.WalletID)
		if reconcileErr == nil && !result.Acquired {
			// A full reconciliation owns the global lease. Leave the hold in
			// place and retry on the next controller cycle.
			gate.cancelProbe()
			continue
		}
		if reconcileErr == nil && result.ReadErrors > 0 {
			reconcileErr = fmt.Errorf("targeted reconciliation completed with %d chain read errors", result.ReadErrors)
		}
		if reconcileErr != nil {
			gate.failure(time.Now().UTC())
			_ = c.repo.SetOperationalStatus(ctx, item.NetworkID, crypto.WalletPoolOperationalStatus{
				ErrorCode:     crypto.WalletPoolErrorChainUnavailable,
				PublicMessage: "TRON RPC временно недоступен. Проверка кошельков будет повторена автоматически.",
			})
			c.log.Warnw("targeted wallet reconciliation failed",
				"wallet_id", item.WalletID,
				"network_id", item.NetworkID,
				"error", reconcileErr,
			)
			continue
		}
		gate.success()
		_ = c.repo.SetOperationalStatus(ctx, item.NetworkID, crypto.WalletPoolHealthyStatus())
		reconciled++
	}
	return reconciled, nil
}

func (c *WalletPoolController) chainGate(networkID id.ID) *dependencyCircuit {
	c.gateMu.Lock()
	defer c.gateMu.Unlock()
	gate := c.chainGates[networkID]
	if gate == nil {
		gate = &dependencyCircuit{}
		c.chainGates[networkID] = gate
	}
	return gate
}

func provisioningBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exponent := math.Min(float64(attempt-1), 8)
	base := min(time.Duration(math.Pow(2, exponent))*time.Second, 5*time.Minute)
	jitter := time.Duration(time.Now().UnixNano() % int64(base/4+1))
	return base + jitter
}

type dependencyCircuit struct {
	failures  int
	openUntil time.Time
	halfOpen  bool
}

func (c *dependencyCircuit) allow(now time.Time) bool {
	if c.openUntil.IsZero() {
		return true
	}
	if now.Before(c.openUntil) {
		return false
	}
	if c.halfOpen {
		return false
	}
	c.halfOpen = true
	return true
}

func (c *dependencyCircuit) success() {
	c.failures = 0
	c.openUntil = time.Time{}
	c.halfOpen = false
}

func (c *dependencyCircuit) failure(now time.Time) {
	c.halfOpen = false
	c.failures++
	if c.failures >= dependencyCircuitThreshold {
		c.openUntil = now.Add(dependencyCircuitOpenFor)
	}
}

func (c *dependencyCircuit) cancelProbe() {
	c.halfOpen = false
}
