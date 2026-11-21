// Package crypto_worker provides the background crypto processing integration.
// It bridges family-neutral chain capabilities to EventProcessor within the worker lifecycle.
//
// The CryptoProcessor is started per-tenant alongside the outbox relay
// and automation scheduler. It manages:
//   - Loading wallet addresses from DB
//   - Starting ManagedWatcher goroutines through family drivers
//   - Consuming BlockchainEvent from watchers → EventProcessor
//   - Invoice expiration ticker (marks expired invoices)
//   - Periodic wallet address refresh (picks up new wallets)
package crypto_worker

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"metapus/internal/configuration/crypto/domain/registers/crypto_merchant_balance"
	"metapus/internal/configuration/crypto/infrastructure/crypto/cryptoregisters"
	"metapus/internal/configuration/crypto/infrastructure/crypto/custody"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_catalog_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/money_movement_repo"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
	infraNumerator "metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

const (
	_addressRefreshPeriod   = 5 * time.Minute
	_expirationCheckPeriod  = 1 * time.Minute
	_confirmationPollPeriod = 10 * time.Second
	_confirmationBatchLimit = 200
	_confirmationLeaseTTL   = 2 * time.Minute
	_sweepEvalPeriod        = 1 * time.Minute
	_sweepEvalBatchSize     = 500
	_reconciliationPeriod   = 15 * time.Minute
	_reconciliationLease    = 5 * time.Minute
)

// CryptoProcessorConfig holds configuration for the crypto processor.
type CryptoProcessorConfig struct {
	// Ready reports successful initialization and subsequent shutdown.
	Ready func(bool)

	// AppEnv controls strict production secret checks.
	AppEnv string

	DriverRegistryBuilder crypto.ChainDriverRegistryBuilder

	// VaultAddr is the HashiCorp Vault endpoint used for outbound signing.
	VaultAddr string

	// VaultToken is allowed only for direct local/dev Vault access. Production
	// uses a tokenless connection to Vault Proxy.
	VaultToken string

	// AuthorizationSigner selects "transit" or "local". Local is forbidden in
	// production and exists only for explicit development/test use.
	AuthorizationSigner   string
	TransitMount          string
	LocalAuthorizationKID string

	// SigningAuthorizationPrivateKey is an Ed25519 private key or seed used
	// only by the explicit local development signer.
	SigningAuthorizationPrivateKey string

	// Approval public keys verify signed money movement approval artifacts.
	ApprovalPolicyPublicKey        string
	IntentSigningPublicKey         string
	RiskAssessmentSigningPublicKey string
	PolicyDecisionSigningPublicKey string
	AdminApprovalSigningPublicKey  string
	RiskApprovalSigningPublicKey   string
	ApprovalPolicyBundleJWS        string
	ApprovalPolicyBundlesJWS       string

	// TenantID is embedded into signing authorizations for Vault scope checks.
	TenantID string

	// JobRecorder records background task execution for the /admin/worker-jobs UI.
	// Optional: if nil, task execution is not recorded.
	JobRecorder *workerjob.Recorder

	// AccountProviderFactory builds provider adapters from database-stored credentials.
	AccountProviderFactory AccountProviderFactory

	// Family-level reusable lease sizing. Vendor HTTP config lives on adapters.
	ResourceLeaseTargetEnergy int64
	ResourceSafetyMarginPct   int64
	ResourceReservationCutoff time.Duration
}

// CryptoProcessor manages crypto payment processing for a single tenant.
// Lifecycle: created per tenant, runs in background, stopped via context cancellation.
type CryptoProcessor struct {
	cfg CryptoProcessorConfig
	log *logger.Logger

	// Repos (stateless — safe for reuse, extract pool from ctx)
	walletRepo     wallet.Repository
	invoiceRepo    crypto_invoice.Repository
	paymentRepo    crypto_payment.Repository
	withdrawalRepo crypto_withdrawal.Repository
	sweepRepo      crypto_sweep.Repository
	treasuryRepo   treasury_withdrawal.Repository
	outboundRepo   *OutboundRepo
	approvalRepo   *money_movement_repo.Repo
	walletPoolRepo crypto.WalletPoolWorkerRepository

	// Domain
	eventProcessor          *crypto.EventProcessor
	walletSvc               *wallet.Service
	sweepResolver           *crypto.SweepConfigResolver
	postingEngine           *posting.Engine
	numerator               numerator.Generator
	merchantBalanceSvc      *crypto_merchant_balance.Service
	webhookPublisher        crypto.LifecycleWebhookPublisher
	walletDerivers          *crypto.FamilyWalletDeriverRegistry
	familySigners           *crypto.FamilySignerRegistry
	drivers                 *crypto.ChainDriverRegistry
	approvalSigners         crypto.MoneyMovementApprovalSignerProvider
	approvalSigningMu       sync.Mutex
	approvalSigningProvider crypto.MoneyMovementApprovalSignerProvider
	resourcePreparers       *FamilyResourcePreparerRegistry
	feeBudget               *FeeBudgetManager
	authorizations          *SigningAuthorizationService
	workerID                string
	balanceReconciler       *cryptoapp.BalanceReconciliationService
	receipts                *receiptIntake

	// Observability
	recorder *workerjob.Recorder

	// Chain watchers per network (for confirmation re-checks)
	mu                   sync.Mutex
	addressWatchers      map[id.ID][]crypto.ManagedWatcher
	networkConfirmations map[id.ID]int
}

// NewCryptoProcessor creates a new crypto processor.
func NewCryptoProcessor(cfg CryptoProcessorConfig, log *logger.Logger) *CryptoProcessor {
	if log == nil {
		log = logger.Default()
	}

	walletRepo := crypto_catalog_repo.NewWalletRepo()
	invoiceRepo := crypto_document_repo.NewCryptoInvoiceRepo()
	paymentRepo := crypto_document_repo.NewCryptoPaymentRepo()
	withdrawalRepo := crypto_document_repo.NewCryptoWithdrawalRepo()
	sweepRepo := crypto_document_repo.NewCryptoSweepRepo()
	treasuryRepo := crypto_document_repo.NewTreasuryWithdrawalRepo()
	outboundRepo := NewOutboundRepo()
	walletPoolRepo := crypto_repo.NewWalletPoolRepo()

	// Build posting engine (for auto-posting confirmed payments)
	// DocLocker is not needed for worker-driven posting — we control the flow.
	postingEngine := posting.NewEngine(nil) // nil locker: worker doesn't use optimistic lock

	// Register crypto visitors + recorders so payments/withdrawals write to registers.
	cryptoRegisters := cryptoregisters.New()
	cryptoRegisters.InstallPosting(postingEngine)

	// Wallet service (for FindByAddress, MarkSweepPending)
	// Numerator is nil-safe: worker doesn't create wallets.
	walletSvc := wallet.NewService(walletRepo, numerator.Noop())

	// Payment FSM
	eventRepo := crypto_repo.NewPaymentEventRepo()
	fsm := crypto.NewPaymentFSM(paymentRepo, eventRepo)

	// Sweep config resolver (token defaults + merchant overrides)
	tokenRepo := crypto_catalog_repo.NewTokenRepo()
	merchantTokenCfgRepo := crypto_repo.NewMerchantTokenConfigRepo()
	sweepResolver := crypto.NewSweepConfigResolver(merchantTokenCfgRepo, tokenRepo)

	// Invoice service with outbox decorator.
	// EventProcessor uses this instead of raw repo so that invoice status changes
	// (overpaid, confirmed, expired) fire automation events via sys_outbox.
	num := infraNumerator.New()
	invoiceService := crypto_invoice.NewService(invoiceRepo, postingEngine, num, contextTxManager{})
	lifecycleWebhookPublisher := crypto_repo.NewLifecycleWebhookOutbox()
	checkoutPublisher := postgres.NewOutboxPublisher()
	invoiceService.OnCreatedInTransaction(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
		invoiceID := doc.ID
		if err := lifecycleWebhookPublisher.PublishLifecycleWebhook(ctx, crypto.LifecycleWebhookEvent{
			Event: crypto.WebhookInvoiceCreated, AggregateID: doc.ID,
			MerchantID: doc.MerchantID, InvoiceID: &invoiceID,
			ResourceVersion: doc.Version, OccurredAt: time.Now().UTC(),
			Data: crypto.InvoiceWebhookData(doc, nil),
		}); err != nil {
			return err
		}
		return publishInvoiceCheckout(ctx, checkoutPublisher, doc)
	})
	outboxPublisher := postgres.NewOutboxPublisher()
	invoiceSvc := domain.Chain[*crypto_invoice.CryptoInvoice](
		domain.WithOutboxEvents[*crypto_invoice.CryptoInvoice]("crypto_invoice", outboxPublisher, nil),
	)(invoiceService)
	txBoundary := contextTxManager{}
	operationalHolds := cryptoapp.NewOperationalHoldService(txBoundary, crypto_repo.NewOperationalHoldRepo())
	externalOutflows := cryptoapp.NewExternalOutflowDetectionService(
		txBoundary,
		crypto_repo.NewExpectedTransferRepo(),
		crypto_repo.NewObservedExternalOutflowRepo(),
		crypto_repo.NewExternalOutflowReconciliationRunRepo(),
		crypto_repo.NewExternalOutflowIncidentRepo(),
		crypto_repo.NewExternalOutflowObservationRepo(),
		crypto_repo.NewExternalOutflowActionRepo(),
		operationalHolds,
	)

	receipts := newReceiptIntake(postingEngine, num)
	// Event Processor — TxManager is extracted from context at runtime.
	ep := crypto.NewEventProcessor(crypto.EventProcessorConfig{
		FSM:               fsm,
		WalletSvc:         walletSvc,
		InvoiceSvc:        invoiceSvc,
		LeaseCloser:       crypto_repo.NewWalletAllocationRepo(),
		Allocations:       crypto_repo.NewWalletAllocationRepo(),
		Inbox:             crypto_repo.NewChainEventInboxRepo(),
		PaymentRepo:       paymentRepo,
		PostingEngine:     postingEngine,
		TxManager:         txBoundary,
		Numerator:         num,
		SweepResolver:     sweepResolver,
		TokenResolver:     tokenRepo,
		FeeResolver:       crypto.NewFeeConfigResolver(crypto_repo.NewFeeScheduleRepo()),
		OutflowRecorder:   externalOutflows,
		ExpectedTransfers: crypto_repo.NewExpectedTransferRepo(),
		FinancialReorgs:   crypto_repo.NewFinancialReorgRepo(),
		FinancialGuard:    crypto_repo.NewBlockIngestionRepo(),
		Receipts:          receipts.service,
		WebhookPublisher:  lifecycleWebhookPublisher,
		OnLatePayment:     func(ctx context.Context) { recordLatePaymentMetric(ctx) },
	})

	appEnv := strings.ToLower(strings.TrimSpace(cfg.AppEnv))
	if appEnv == "production" && strings.TrimSpace(cfg.VaultToken) != "" {
		panic("VAULT_TOKEN is forbidden in production; use tokenless Vault Proxy mode")
	}
	if appEnv == "production" && strings.TrimSpace(cfg.SigningAuthorizationPrivateKey) != "" {
		panic("CRYPTO_SIGNING_AUTH_PRIVATE_KEY is forbidden in production; use Vault Transit")
	}

	var sharedVaultClient *vaultclient.Client
	if strings.TrimSpace(cfg.VaultAddr) != "" {
		var err error
		sharedVaultClient, err = vaultclient.New(vaultclient.Config{Addr: cfg.VaultAddr, Token: cfg.VaultToken})
		if err != nil {
			panic(fmt.Sprintf("invalid Vault client configuration: %v", err))
		}
	}

	familySigners, walletDerivers, err := custody.Build(custody.Config{Client: sharedVaultClient, TenantID: cfg.TenantID})
	if err != nil {
		panic(fmt.Sprintf("invalid blockchain custody configuration: %v", err))
	}

	authorizationSigner, err := configuredAuthorizationSigner(cfg, sharedVaultClient)
	if err != nil {
		panic(fmt.Sprintf("invalid signing authorization configuration: %v", err))
	}

	var authorizations *SigningAuthorizationService
	if authorizationSigner != nil {
		var err error
		authorizations, err = NewSigningAuthorizationService(outboundRepo, authorizationSigner, cfg.TenantID)
		if err != nil {
			panic(fmt.Sprintf("invalid signing authorization service configuration: %v", err))
		}
	}

	resourcePreparers := NewFamilyResourcePreparerRegistry()
	feeBudget := NewFeeBudgetManager()
	if cfg.AccountProviderFactory != nil {
		if err := resourcePreparers.Register(crypto.ChainFamilyTRON, NewSharedResourcePreparer(SharedResourcePreparerConfig{
			Store:                  outboundRepo,
			AccountProviderFactory: cfg.AccountProviderFactory,
			Logger:                 log,
			TargetEnergy:           cfg.ResourceLeaseTargetEnergy,
			SafetyMarginPct:        cfg.ResourceSafetyMarginPct,
			ReservationCutoff:      cfg.ResourceReservationCutoff,
		})); err != nil {
			panic(fmt.Sprintf("register TRON resource preparer: %v", err))
		}
	}

	var approvalSigners crypto.MoneyMovementApprovalSignerProvider
	approvalKeys, err := approvalPublicKeysFromConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("invalid money movement approval public key configuration: %v", err))
	}
	approvalSigners, err = crypto.NewStaticMoneyMovementApprovalVerifierProvider(approvalKeys)
	if err != nil {
		panic(fmt.Sprintf("failed to create money movement approval verifier: %v", err))
	}

	processor := &CryptoProcessor{
		cfg:                  cfg,
		log:                  log.WithComponent("crypto"),
		walletRepo:           walletRepo,
		invoiceRepo:          invoiceRepo,
		paymentRepo:          paymentRepo,
		withdrawalRepo:       withdrawalRepo,
		sweepRepo:            sweepRepo,
		treasuryRepo:         treasuryRepo,
		outboundRepo:         outboundRepo,
		approvalRepo:         money_movement_repo.New(),
		walletPoolRepo:       walletPoolRepo,
		eventProcessor:       ep,
		receipts:             receipts,
		walletSvc:            walletSvc,
		sweepResolver:        sweepResolver,
		postingEngine:        postingEngine,
		numerator:            num,
		merchantBalanceSvc:   cryptoRegisters.MerchantBalance,
		webhookPublisher:     lifecycleWebhookPublisher,
		walletDerivers:       walletDerivers,
		familySigners:        familySigners,
		approvalSigners:      approvalSigners,
		resourcePreparers:    resourcePreparers,
		feeBudget:            feeBudget,
		authorizations:       authorizations,
		workerID:             cfg.TenantID + ":" + id.New().String(),
		recorder:             cfg.JobRecorder,
		addressWatchers:      make(map[id.ID][]crypto.ManagedWatcher),
		networkConfirmations: make(map[id.ID]int),
	}
	receipts.paymentReorg = processor.reorgReceiptPayment
	return processor
}

// Start begins crypto processing. Blocks until ctx is cancelled.
// Starts per-network watchers + event consumer + maintenance tickers.
func (p *CryptoProcessor) Start(ctx context.Context) error {
	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// The supervisor creates a fresh processor for the next attempt. RPC pools
	// belong to this attempt and are closed only after its goroutines have drained.
	defer func() {
		if p.drivers != nil {
			for _, driver := range p.drivers.Drivers() {
				if closer, ok := driver.(interface{ Close() }); ok {
					closer.Close()
				}
			}
		}
	}()
	if p.cfg.Ready != nil {
		p.cfg.Ready(false)
		defer p.cfg.Ready(false)
	}
	// Only watched networks are read. Starting or stopping watching restarts
	// the attempt instead of waiting for a worker restart.
	initialNetworkSet, _ := p.loadNetworkSetSignature(ctx)
	var networkSetChanged atomic.Bool
	var networkSetWatch sync.WaitGroup
	networkSetWatch.Go(func() {
		watchNetworkSet(ctx, networkSetCheckPeriod, initialNetworkSet, p.loadNetworkSetSignature, &networkSetChanged, cancel)
	})
	defer networkSetWatch.Wait()
	defer cancel()
	attemptResult := func(err error) error {
		if networkSetChanged.Load() && parentCtx.Err() == nil {
			return errors.Join(err, errNetworkSetChanged)
		}
		return err
	}
	networks, err := p.initializeChainRuntime(ctx)
	if err != nil {
		if len(networks) == 0 {
			p.log.Errorw("failed to load blockchain networks", "error", err)
			return err
		}
		p.log.Errorw("one or more blockchain networks are unavailable; healthy networks continue", "error", err)
	}

	startupErr := err
	if len(networks) == 0 {
		if p.cfg.Ready != nil {
			p.cfg.Ready(true)
		}
		p.log.Info("no active blockchain networks found, crypto processing idle")
		var maintenance sync.WaitGroup
		maintenance.Go(func() { p.runApprovalSigningLoop(ctx) })
		maintenance.Go(func() { p.runInvoiceReconciliationLoop(ctx) })
		p.runExpirationLoop(ctx)
		maintenance.Wait()
		return attemptResult(parentCtx.Err())
	}
	var watcherWg sync.WaitGroup
	var consumerWg sync.WaitGroup

	// Start chain watchers through family-neutral capabilities.
	for _, net := range networks {
		driver, resolveErr := p.drivers.Resolve(net.ref.ID)
		if resolveErr != nil {
			startupErr = errors.Join(startupErr, resolveErr)
			p.log.Errorw("network driver unavailable", "network_id", net.ref.ID, "error", resolveErr)
			continue
		}
		addresses, addressErr := p.loadWalletAddresses(ctx, net.ref.ID)
		if addressErr != nil {
			startupErr = errors.Join(startupErr, addressErr)
			p.log.Errorw("failed to load watcher addresses", "network_id", net.ref.ID, "error", addressErr)
			continue
		}
		if factory, ok := driver.(crypto.NetworkWatcherFactory); ok {
			watchers, watcherErr := factory.NetworkWatchers(ctx, net.ref, net.assets, addresses)
			if watcherErr != nil {
				startupErr = errors.Join(startupErr, watcherErr)
				p.log.Errorw("failed to create network watchers", "network_id", net.ref.ID, "error", watcherErr)
				continue
			}
			for _, watcher := range watchers {
				p.registerManagedWatcher(net.ref.ID, watcher)
				watcherWg.Add(1)
				go p.runManagedWatcher(ctx, &watcherWg, net.ref, watcher)
			}
			continue
		}
		if driver.Watchers() == nil {
			startupErr = errors.Join(startupErr, fmt.Errorf("network %s has no watcher factory", net.ref.Code))
			continue
		}
		nativeWatcher, watcherErr := driver.Watchers().NewNativeWatcher(ctx, net.ref, addresses)
		if watcherErr != nil {
			startupErr = errors.Join(startupErr, watcherErr)
			p.log.Errorw("failed to create native watcher", "network_id", net.ref.ID, "error", watcherErr)
		} else {
			p.registerManagedWatcher(net.ref.ID, nativeWatcher)
			watcherWg.Add(1)
			go p.runManagedWatcher(ctx, &watcherWg, net.ref, nativeWatcher)
		}
		for _, asset := range net.assets {
			if asset.Standard == token.TokenStandardNative {
				continue
			}
			watcher, watcherErr := driver.Watchers().NewTokenWatcher(ctx, net.ref, asset, addresses)
			if watcherErr != nil {
				startupErr = errors.Join(startupErr, watcherErr)
				p.log.Errorw("failed to create token watcher", "network_id", net.ref.ID, "token_id", asset.TokenID, "error", watcherErr)
				continue
			}
			p.registerManagedWatcher(net.ref.ID, watcher)
			watcherWg.Add(1)
			go p.runManagedWatcher(ctx, &watcherWg, net.ref, watcher)
		}
	}

	if startupErr != nil {
		// Keep healthy networks running during the retry delay, then drain the
		// entire attempt before rebuilding failed drivers and watcher factories.
		retry := time.AfterFunc(time.Minute, cancel)
		defer retry.Stop()
	} else if p.cfg.Ready != nil {
		p.cfg.Ready(true)
	}

	consumerWg.Go(func() { p.runAMLJobLoop(ctx) })

	// Start expiration ticker
	consumerWg.Go(func() {
		p.runExpirationLoop(ctx)
	})
	consumerWg.Go(func() {
		p.runWatcherLagLeaseBlockLoop(ctx)
	})
	consumerWg.Go(func() {
		p.runInboxRetryLoop(ctx)
	})
	consumerWg.Go(func() {
		p.runChainObservationLoop(ctx)
	})
	consumerWg.Go(func() {
		p.runApprovalSigningLoop(ctx)
	})
	consumerWg.Go(func() {
		p.runInvoiceReconciliationLoop(ctx)
	})

	// Refresh the managed-wallet set of every running watcher. This includes
	// wallets created after worker startup and requires no watcher restart.
	consumerWg.Go(func() {
		p.runAddressRefreshLoop(ctx)
	})

	// Publish sanitized runtime readiness for guarded Admin enablement.
	consumerWg.Go(func() {
		p.runNetworkRuntimeStatusLoop(ctx)
	})

	// Start confirmation poll loop (re-checks detected/confirming payments)
	consumerWg.Go(func() {
		p.runConfirmationLoop(ctx)
	})
	// Account final unmatched incoming transfers as custody receipts.
	consumerWg.Go(func() {
		p.runReceiptIntakeLoop(ctx)
	})
	consumerWg.Go(func() {
		p.runReceiptAuditLoop(ctx)
	})

	// Start sweep evaluation loop (checks accumulated balances against thresholds)
	consumerWg.Go(func() {
		p.runSweepEvaluationLoop(ctx)
	})

	// Start outbound confirmation loop for already-broadcast withdrawals/sweeps.
	if p.drivers != nil {
		p.mustValidateOutboundDependencies()
		consumerWg.Go(func() {
			p.runOutboundConfirmationLoop(ctx)
		})
		consumerWg.Go(func() {
			p.runReconciliationLoop(ctx)
		})
	}

	// Outbound execution is fail-safe: disabled unless Vault signing is configured.
	if p.drivers != nil && p.familySigners != nil && p.familySigners.Len() > 0 && p.authorizations != nil && p.cfg.TenantID != "" {
		p.mustValidateOutboundDependencies()
		walletPoolController := NewWalletPoolController(
			p.walletPoolRepo,
			p.drivers,
			p.RunWalletBalanceReconciliation,
			p.workerID,
			p.recorder,
			p.log,
		)
		consumerWg.Go(func() {
			walletPoolController.runLoop(ctx)
		})

		accountActivationExecutor := NewAccountActivationExecutor(
			p.drivers,
			p.familySigners,
			p.outboundRepo,
			p.authorizations,
			p.feeBudget,
			func() *CryptoNetworkOperationCoordinator {
				repo := crypto_document_repo.NewCryptoNetworkOperationRepo()
				service := crypto_network_operation.NewService(repo, p.postingEngine, p.numerator, nil)
				return NewCryptoNetworkOperationCoordinator(repo, service, p.feeBudget)
			}(),
			p.walletPoolRepo,
			p.cfg.TenantID,
			p.recorder,
			p.log,
		)
		consumerWg.Go(func() {
			accountActivationExecutor.runLoop(ctx)
		})

		withdrawalExecutor := NewWithdrawalExecutor(
			p.drivers,
			p.familySigners,
			p.withdrawalRepo,
			p.outboundRepo,
			p.approvalRepo,
			p.postingEngine,
			p.numerator,
			p.merchantBalanceSvc,
			p.resourcePreparers,
			p.feeBudget,
			p.approvalSigners,
			p.authorizations,
			p.cfg.TenantID,
			p.log,
		)
		consumerWg.Go(func() {
			withdrawalExecutor.runLoop(ctx)
		})

		sweepExecutor := NewSweepExecutor(
			p.drivers,
			p.familySigners,
			p.sweepRepo,
			p.outboundRepo,
			p.postingEngine,
			p.numerator,
			p.resourcePreparers,
			p.feeBudget,
			p.authorizations,
			p.cfg.TenantID,
			p.log,
		)
		consumerWg.Go(func() {
			sweepExecutor.runLoop(ctx)
		})

		treasuryExecutor := NewTreasuryWithdrawalExecutor(
			p.drivers,
			p.familySigners,
			p.treasuryRepo,
			p.outboundRepo,
			p.approvalRepo,
			p.postingEngine,
			p.numerator,
			p.resourcePreparers,
			p.feeBudget,
			p.approvalSigners,
			p.authorizations,
			p.cfg.TenantID,
			p.log,
		)
		consumerWg.Go(func() {
			treasuryExecutor.runLoop(ctx)
		})
	} else {
		p.log.Warn("outbound withdrawal, sweep, and treasury execution disabled: Vault signing or authorization is not configured")
	}

	p.log.Infow("crypto processor started",
		"networks", len(networks),
	)

	// Wait for consumer + expiration to finish
	consumerWg.Wait()
	watcherWg.Wait()

	p.log.Info("crypto processor stopped")
	return attemptResult(errors.Join(startupErr, parentCtx.Err()))
}

func (p *CryptoProcessor) mustValidateOutboundDependencies() {
	if p.merchantBalanceSvc == nil {
		panic("crypto_worker.CryptoProcessor: merchant balance service is required for outbound settlement")
	}
}

func (p *CryptoProcessor) runManagedWatcher(ctx context.Context, wg *sync.WaitGroup, network crypto.NetworkRef, watcher crypto.ManagedWatcher) {
	defer wg.Done()
	p.log.Infow("starting chain watcher", "network_id", network.ID, "chain_family", network.Family)
	workerjob.Supervise(ctx, 5*time.Second, time.Minute, func(runCtx context.Context) error {
		return watcher.Start(runCtx, p)
	}, func(err error, delay time.Duration) {
		p.log.Warnw("chain watcher restarting", "network_id", network.ID, "error", err, "retry_after", delay)
	})
}

func (p *CryptoProcessor) registerManagedWatcher(networkID id.ID, watcher crypto.ManagedWatcher) {
	p.mu.Lock()
	p.addressWatchers[networkID] = append(p.addressWatchers[networkID], watcher)
	p.mu.Unlock()
}

func (p *CryptoProcessor) runAddressRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(_addressRefreshPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.refreshWalletAddresses(ctx); err != nil {
				p.log.Errorw("failed to refresh chain watcher addresses", "error", err)
			}
		}
	}
}

func (p *CryptoProcessor) refreshWalletAddresses(ctx context.Context) error {
	p.mu.Lock()
	watchersByNetwork := make(map[id.ID][]crypto.ManagedWatcher, len(p.addressWatchers))
	for networkID, watchers := range p.addressWatchers {
		watchersByNetwork[networkID] = append([]crypto.ManagedWatcher(nil), watchers...)
	}
	p.mu.Unlock()

	for networkID, watchers := range watchersByNetwork {
		addresses, err := p.loadWalletAddresses(ctx, networkID)
		if err != nil {
			return fmt.Errorf("load wallet addresses for network %s: %w", networkID, err)
		}
		for _, watcher := range watchers {
			watcher.ReplaceMonitoredAddresses(addresses)
			if reporter, ok := watcher.(crypto.AddressCoverageReporter); ok {
				if coverage, ready := reporter.AddressCoverage(); ready {
					if coverage.NetworkID != networkID {
						return fmt.Errorf("watcher coverage network changed")
					}
					if err := crypto_repo.NewAMLRepository().RecordAMLAddressCoverage(ctx, coverage, addresses); err != nil {
						return err
					}
				}
			}
		}
		p.log.Debugw("refreshed chain watcher addresses",
			"network_id", networkID,
			"watchers", len(watchers),
			"addresses", len(addresses),
		)
	}
	return nil
}

// Process acknowledges an event only after its raw observation is durable.
// The observation worker performs the business transaction separately.
func (p *CryptoProcessor) Process(ctx context.Context, event crypto.BlockchainEvent) error {
	if err := p.captureChainObservation(ctx, event); err != nil {
		p.log.Errorw("failed to capture blockchain observation",
			"tx_hash", event.TxHash,
			"network_id", event.NetworkID,
			"error", err,
		)
		return err
	}
	return nil
}

// runExpirationLoop periodically checks for expired invoices and marks them.
func (p *CryptoProcessor) runExpirationLoop(ctx context.Context) {
	ticker := time.NewTicker(_expirationCheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.recorder != nil {
				// RecordIfWork: only log when invoices actually expired (idle ticks skipped).
				p.recorder.RecordIfWork(ctx, "crypto.expiration", "crypto", func(ctx context.Context) (int, error) {
					n, err := p.expireInvoicesWithCount(ctx)
					if n > 0 {
						p.log.Infow("expired invoices", "count", n)
					}
					return n, err
				})
			} else if err := p.expireInvoices(ctx); err != nil {
				p.log.Errorw("failed to expire invoices", "error", err)
			}
		}
	}
}

// expireInvoices marks invoices that have passed their expiration time.
// Delegates to repository layer to maintain Clean Architecture boundary.
func (p *CryptoProcessor) expireInvoices(ctx context.Context) error {
	count, err := p.expireInvoicesWithCount(ctx)
	if err != nil {
		return fmt.Errorf("expire invoices: %w", err)
	}

	if count > 0 {
		p.log.Infow("expired invoices",
			"count", count,
		)
	}

	return nil
}

func (p *CryptoProcessor) expireInvoicesWithCount(ctx context.Context) (int, error) {
	candidates, err := p.invoiceRepo.FindOverdueIDs(ctx, 500)
	if err != nil {
		return 0, fmt.Errorf("find overdue invoices: %w", err)
	}
	count := 0
	for _, invoiceID := range candidates {
		err := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
			releaseWallet, err := p.eventProcessor.ExpireInvoiceInTransaction(txCtx, invoiceID)
			if err != nil {
				return err
			}
			if !releaseWallet {
				return nil
			}
			// InvoiceLifecycleService still holds the invoice row lock here. Lock
			// the wallet afterwards so every path uses invoice -> wallet and a
			// payment confirmation cannot deadlock with expiration maintenance.
			if err := p.invoiceRepo.LockWalletForInvoice(txCtx, invoiceID); err != nil {
				return err
			}
			if err := p.invoiceRepo.ReleaseInvoiceWallet(txCtx, invoiceID, "expired"); err != nil {
				return err
			}
			count++
			return nil
		})
		if err != nil {
			return count, fmt.Errorf("expire invoice %s: %w", invoiceID, err)
		}
	}
	if err := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		return p.invoiceRepo.CleanupOrphanedExpiredLeases(txCtx)
	}); err != nil {
		return count, err
	}
	return count, nil
}

// runConfirmationLoop polls family finality readers. Confirmation counts remain
// telemetry; only a solidified/finalized receipt may cause financial posting.
func (p *CryptoProcessor) runConfirmationLoop(ctx context.Context) {
	ticker := time.NewTicker(_confirmationPollPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.pollConfirmations(ctx); err != nil {
				p.log.Errorw("failed to poll confirmations", "error", err)
			}
		}
	}
}

// pollConfirmations leases a bounded detected/confirming batch and resolves the
// receipt through the exact network_id stored on the payment.
func (p *CryptoProcessor) pollConfirmations(ctx context.Context) error {
	payments, err := p.paymentRepo.ClaimForConfirmation(
		ctx,
		[]crypto_payment.PaymentStatus{
			crypto_payment.PaymentStatusConfirming,
			crypto_payment.PaymentStatusDetected,
		},
		p.workerID,
		_confirmationBatchLimit,
		_confirmationLeaseTTL,
	)
	if err != nil {
		return fmt.Errorf("claim payments for confirmation: %w", err)
	}

	if len(payments) == 0 {
		return nil
	}

	p.log.Debugw("confirmation poll: found pending payments",
		"count", len(payments),
	)

	for _, payment := range payments {
		func(payment *crypto_payment.CryptoPayment) {
			defer func() {
				if err := p.paymentRepo.ReleaseConfirmationClaim(ctx, payment.ID, p.workerID); err != nil {
					p.log.Errorw("failed to release confirmation claim",
						"payment_id", payment.ID,
						"tx_hash", payment.TxHash,
						"error", err,
					)
				}
			}()

			if p.drivers == nil {
				return
			}
			driver, resolveErr := p.drivers.Resolve(payment.NetworkID)
			if resolveErr != nil {
				p.log.Errorw("payment network driver unavailable", "payment_id", payment.ID, "network_id", payment.NetworkID, "error", resolveErr)
				return
			}
			if verifier, ok := driver.(crypto.TransferVerifier); ok && verifier.VerificationRequired() {
				repo := crypto_repo.NewBlockIngestionRepo()
				expected, readErr := repo.ExpectedTransfer(ctx, payment)
				if readErr != nil {
					p.log.Warnw("payment transfer reference unavailable", "payment_id", payment.ID, "error", readErr)
					return
				}
				proof, verifyErr := verifier.VerifyTransfer(ctx, expected)
				if verifyErr != nil {
					if errors.Is(verifyErr, crypto.ErrVerificationMismatch) {
						if err := repo.ReportIngestionFailure(ctx, payment.NetworkID, "confirmation", payment.BlockNumber, "transfer_mismatch", true); err != nil {
							p.log.Errorw("payment evidence mismatch incident capture failed", "payment_id", payment.ID, "error", err)
						}
					}
					p.log.Warnw("payment transfer verification waiting", "payment_id", payment.ID, "error", verifyErr)
					return
				}
				proofID, saveErr := repo.SaveVerification(ctx, proof)
				if saveErr != nil {
					p.log.Errorw("payment transfer evidence capture failed", "payment_id", payment.ID, "error", saveErr)
					return
				}
				event := proof.Event
				event.VerificationID = &proofID
				if processErr := p.eventProcessor.ProcessEvent(ctx, event); processErr != nil {
					if errors.Is(processErr, crypto.ErrVerificationMismatch) {
						if err := repo.ReportIngestionFailure(ctx, payment.NetworkID, "confirmation", payment.BlockNumber, "transfer_mismatch", true); err != nil {
							p.log.Errorw("verified payment conflict incident capture failed", "payment_id", payment.ID, "error", err)
						}
					}
					p.log.Errorw("verified payment processing failed", "payment_id", payment.ID, "error", processErr)
				}
				return
			}
			receipt, receiptErr := driver.Finality().Receipt(ctx, payment.TxHash)
			if receiptErr != nil {
				p.log.Warnw("payment finality read failed", "payment_id", payment.ID, "network_id", payment.NetworkID, "error", receiptErr)
				return
			}
			if receipt.Status == crypto.ReceiptPending {
				// A receipt can disappear after a provisional observation. Do
				// not call that a reorg merely because one provider returned
				// not-found: compare the payment's block hash with the canonical
				// block at the same height first.
				if payment.BlockHash != "" && payment.BlockNumber >= 0 {
					canonical, ok := driver.(crypto.CanonicalBlockReader)
					if ok {
						canonicalHash, canonicalErr := canonical.CanonicalBlock(ctx, payment.BlockNumber)
						if canonicalErr != nil {
							p.log.Warnw("payment canonical block read failed", "payment_id", payment.ID,
								"network_id", payment.NetworkID, "block_number", payment.BlockNumber, "error", canonicalErr)
							return
						}
						if !strings.EqualFold(canonicalHash, payment.BlockHash) {
							event := crypto.BlockchainEvent{NetworkID: payment.NetworkID, TxHash: payment.TxHash,
								EventIndex: payment.EventIndex, EventKind: crypto.EventKind(payment.EventKind), Amount: payment.Amount,
								BlockNumber: payment.BlockNumber, BlockHash: canonicalHash, Finality: crypto.FinalityProvisional,
								Canonicality: crypto.CanonicalityOrphaned, Confirmations: 0, RequiredConfs: payment.RequiredConfs,
								EventType: crypto.EventTypeReorg}
							if processErr := p.eventProcessor.ProcessEvent(ctx, event); processErr != nil {
								p.log.Errorw("failed to process orphaned payment", "payment_id", payment.ID,
									"network_id", payment.NetworkID, "error", processErr)
							}
						}
					}
				}
				return
			}
			if receipt.Status == crypto.ReceiptReverted {
				p.log.Errorw("incoming payment transaction reports execution revert; treating as execution anomaly, not reorg",
					"payment_id", payment.ID, "network_id", payment.NetworkID, "tx_hash", payment.TxHash)
				return
			}
			eventType := crypto.EventTypeConfirmation
			if receipt.Canonicality == crypto.CanonicalityOrphaned ||
				(payment.BlockHash != "" && receipt.BlockHash != "" && payment.BlockNumber == receipt.BlockHeight && payment.BlockHash != receipt.BlockHash) {
				eventType = crypto.EventTypeReorg
			}
			event := crypto.BlockchainEvent{
				NetworkID:     payment.NetworkID,
				TxHash:        payment.TxHash,
				EventIndex:    payment.EventIndex,
				EventKind:     crypto.EventKind(payment.EventKind),
				Amount:        payment.Amount,
				BlockNumber:   receipt.BlockHeight,
				BlockHash:     receipt.BlockHash,
				Finality:      receipt.Finality,
				Canonicality:  receipt.Canonicality,
				Confirmations: receipt.Confirmations,
				RequiredConfs: payment.RequiredConfs,
				EventType:     eventType,
			}

			if err := p.eventProcessor.ProcessEvent(ctx, event); err != nil {
				p.log.Errorw("failed to process confirmation update",
					"payment_id", payment.ID,
					"tx_hash", payment.TxHash,
					"finality", receipt.Finality,
					"error", err,
				)
			}
		}(payment)
	}

	return nil
}

// ── Sweep Evaluation ────────────────────────────────────────────────────

// runSweepEvaluationLoop periodically checks pool wallets for threshold-based sweep.
// Wallets with accumulated balance >= threshold (or exceeding max age) are marked sweep_pending.
func (p *CryptoProcessor) runSweepEvaluationLoop(ctx context.Context) {
	ticker := time.NewTicker(_sweepEvalPeriod)
	defer ticker.Stop()

	p.log.Info("sweep evaluation loop started")

	for {
		select {
		case <-ctx.Done():
			p.log.Info("sweep evaluation loop stopped")
			return
		case <-ticker.C:
			if p.recorder != nil {
				// RecordIfWork: only log when wallets are actually marked sweep_pending.
				p.recorder.RecordIfWork(ctx, "crypto.sweep_eval", "crypto", func(ctx context.Context) (int, error) {
					return p.evaluateSweepsWithCount(ctx)
				})
			} else if err := p.evaluateSweeps(ctx); err != nil {
				p.log.Errorw("sweep evaluation failed", "error", err)
			}
		}
	}
}

// sweepCandidate holds wallet info with its accumulated balance metadata.
type sweepCandidate struct {
	walletID        id.ID
	merchantID      *id.ID
	tokenID         id.ID
	balance         types.RawAmount
	lastSweptAt     *time.Time
	oldestPaymentAt *time.Time // MIN(confirmed_at) — for max-age check on never-swept wallets
}

// evaluateSweeps queries pool wallets and checks if any should be swept.
func (p *CryptoProcessor) evaluateSweeps(ctx context.Context) error {
	candidates, err := p.querySweepCandidates(ctx)
	if err != nil {
		return err
	}
	_, err = p.evaluateSweepCandidates(ctx, candidates)
	return err
}

// evaluateSweepsWithCount is a wrapper for recorder.Record — returns the number of wallets marked sweep_pending.
func (p *CryptoProcessor) evaluateSweepsWithCount(ctx context.Context) (int, error) {
	candidates, err := p.querySweepCandidates(ctx)
	if err != nil {
		return 0, err
	}
	return p.evaluateSweepCandidates(ctx, candidates)
}

func (p *CryptoProcessor) querySweepCandidates(ctx context.Context) ([]sweepCandidate, error) {
	rows, err := crypto_repo.NewWorkerRuntimeRepo().QuerySweepCandidates(ctx, _sweepEvalBatchSize)
	if err != nil {
		return nil, err
	}
	items := make([]sweepCandidate, 0, len(rows))
	for _, row := range rows {
		items = append(items, sweepCandidate{walletID: row.WalletID, merchantID: row.MerchantID, tokenID: row.TokenID, balance: row.Balance, lastSweptAt: row.LastSweptAt, oldestPaymentAt: row.OldestPaymentAt})
	}
	return items, nil
}

// evaluateSweepCandidates checks each candidate against its sweep config and marks eligible wallets.
// Returns the number of wallets marked sweep_pending.
func (p *CryptoProcessor) evaluateSweepCandidates(ctx context.Context, candidates []sweepCandidate) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}

	p.log.Infow("evaluating sweep candidates", "count", len(candidates))

	var marked int
	for _, c := range candidates {
		merchantID := id.Nil()
		if c.merchantID != nil {
			merchantID = *c.merchantID
		}

		// Resolve effective sweep config (merchant override → token default)
		cfg, err := p.sweepResolver.Resolve(ctx, merchantID, c.tokenID)
		if err != nil {
			p.log.Errorw("failed to resolve sweep config",
				"wallet_id", c.walletID,
				"token_id", c.tokenID,
				"error", err,
			)
			continue
		}

		// Skip zero-threshold — those wallets are already handled immediately in EventProcessor
		if cfg.IsZeroThreshold() {
			continue
		}

		sweepable := cfg.SweepableAmount(c.balance)
		if !sweepable.IsPositive() {
			continue
		}

		// Check threshold against the amount that can be swept after retaining dust.
		thresholdMet := sweepable.Cmp(cfg.Threshold) >= 0

		// Check max age (if configured)
		ageMet := false
		if cfg.MaxAgeHours > 0 {
			maxAge := time.Duration(cfg.MaxAgeHours) * time.Hour
			if c.lastSweptAt != nil {
				ageMet = time.Since(*c.lastSweptAt) > maxAge
			} else if c.oldestPaymentAt != nil {
				// Never swept: check age from oldest unswept confirmed payment.
				ageMet = time.Since(*c.oldestPaymentAt) > maxAge
			}
		}

		if thresholdMet || ageMet {
			p.log.Infow("marking wallet for sweep",
				"wallet_id", c.walletID,
				"balance", c.balance.String(),
				"sweepable", sweepable.String(),
				"retain", cfg.RetainAmount.String(),
				"threshold", cfg.Threshold.String(),
				"threshold_met", thresholdMet,
				"age_met", ageMet,
			)

			if err := p.walletSvc.BeginSweep(ctx, c.walletID); err != nil {
				p.log.Errorw("failed to mark wallet sweep pending",
					"wallet_id", c.walletID,
					"error", err,
				)
				continue
			}
			marked++
		}
	}

	return marked, nil
}

// ── Internal helpers ────────────────────────────────────────────────────

type networkInfo struct {
	ref    crypto.NetworkRef
	assets []crypto.AssetRef
}

// InitializeChainRuntime validates and builds all configured network drivers.
// Deployment preflight uses the same composition path as the long-running worker.
func (p *CryptoProcessor) InitializeChainRuntime(ctx context.Context) error {
	_, err := p.initializeChainRuntime(ctx)
	return err
}

// InitializeNetworkPreflight validates one catalog network even while it is
// inactive. This is the deployment gate used before watcher-only activation.
func (p *CryptoProcessor) InitializeNetworkPreflight(ctx context.Context, networkCode string) error {
	networks, err := p.loadNetworksFiltered(ctx, false, strings.TrimSpace(networkCode))
	if err != nil {
		return err
	}
	if len(networks) != 1 {
		return fmt.Errorf("preflight network %q was not found or is ambiguous", networkCode)
	}
	healthy, err := p.buildChainRuntime(ctx, networks)
	if err != nil {
		return err
	}
	for _, network := range healthy {
		if _, err := resolveOutboundSigner(ctx, p.familySigners, p.authorizations, network.ref); err != nil {
			return fmt.Errorf("custody preflight %s: %w", network.ref.Code, err)
		}
	}
	return nil
}

func (p *CryptoProcessor) initializeChainRuntime(ctx context.Context) ([]networkInfo, error) {
	networks, err := p.loadNetworks(ctx)
	if err != nil || len(networks) == 0 {
		return networks, err
	}
	return p.buildChainRuntime(ctx, networks)
}

func (p *CryptoProcessor) buildChainRuntime(ctx context.Context, networks []networkInfo) ([]networkInfo, error) {
	if p.cfg.DriverRegistryBuilder == nil || p.walletDerivers == nil {
		return nil, fmt.Errorf("chain driver registry and Vault wallet deriver are required")
	}
	refs := make([]crypto.NetworkRef, 0, len(networks))
	for _, network := range networks {
		refs = append(refs, network.ref)
		p.networkConfirmations[network.ref.ID] = network.ref.RequiredConfirmations
	}
	drivers, err := p.cfg.DriverRegistryBuilder.Build(ctx, refs, p.walletDerivers)
	p.drivers = drivers
	buildErr := err
	if p.drivers == nil || len(p.drivers.Drivers()) == 0 {
		return nil, fmt.Errorf("build chain driver registry: %w", buildErr)
	}
	healthy := make([]networkInfo, 0, len(networks))
	var preflightErrors []error
	for _, network := range networks {
		driver, resolveErr := p.drivers.Resolve(network.ref.ID)
		if resolveErr != nil {
			preflightErrors = append(preflightErrors, resolveErr)
			continue
		}
		if reconciler, ok := driver.(crypto.NetworkStartupReconciler); ok {
			if reconcileErr := reconciler.ReconcileStartup(ctx); reconcileErr != nil {
				preflightErrors = append(preflightErrors, fmt.Errorf("startup reconciliation %s: %w", network.ref.Code, reconcileErr))
				continue
			}
		}
		if preflighter, ok := driver.(crypto.NetworkRuntimePreflighter); ok {
			if preflightErr := preflighter.Preflight(ctx, network.assets); preflightErr != nil {
				preflightErrors = append(preflightErrors, fmt.Errorf("preflight %s: %w", network.ref.Code, preflightErr))
				continue
			}
		}
		healthy = append(healthy, network)
	}
	return healthy, errors.Join(buildErr, errors.Join(preflightErrors...))
}

// loadNetworks resolves immutable network/asset identities from the catalog.
func (p *CryptoProcessor) loadNetworks(ctx context.Context) ([]networkInfo, error) {
	return p.loadNetworksFiltered(ctx, true, "")
}

func (p *CryptoProcessor) loadNetworksFiltered(ctx context.Context, activeOnly bool, networkCode string) ([]networkInfo, error) {
	rows, err := crypto_repo.NewWorkerRuntimeRepo().LoadNetworksFiltered(ctx, activeOnly, networkCode)
	if err != nil {
		return nil, err
	}
	var networks []networkInfo
	for _, row := range rows {
		networks = append(networks, networkInfo{ref: row.Ref, assets: row.Assets})
	}
	return networks, nil
}

func (p *CryptoProcessor) loadWalletAddresses(ctx context.Context, networkID id.ID) ([]string, error) {
	return crypto_repo.NewWorkerRuntimeRepo().LoadWalletAddresses(ctx, networkID)
}

// contextTxManager implements tx.Manager by delegating to the TxManager
// already stored in the request context (set by Worker's runTenantWorker).
type contextTxManager struct{}

func (contextTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, fn)
}

// Compile-time check.
var _ tx.Manager = contextTxManager{}

func approvalPublicKeysFromConfig(cfg CryptoProcessorConfig) (crypto.ApprovalPublicKeys, error) {
	policyKey, err := parseRequiredApprovalPublicKey("CRYPTO_APPROVAL_POLICY_PUBLIC_KEY", cfg.ApprovalPolicyPublicKey)
	if err != nil {
		return crypto.ApprovalPublicKeys{}, err
	}
	intentKey, err := parseRequiredApprovalPublicKey("CRYPTO_INTENT_SIGNING_PUBLIC_KEY", cfg.IntentSigningPublicKey)
	if err != nil {
		return crypto.ApprovalPublicKeys{}, err
	}
	riskKey, err := parseRequiredApprovalPublicKey("CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY", cfg.RiskAssessmentSigningPublicKey)
	if err != nil {
		return crypto.ApprovalPublicKeys{}, err
	}
	decisionKey, err := parseRequiredApprovalPublicKey("CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY", cfg.PolicyDecisionSigningPublicKey)
	if err != nil {
		return crypto.ApprovalPublicKeys{}, err
	}
	adminKey, err := parseRequiredApprovalPublicKey("CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY", cfg.AdminApprovalSigningPublicKey)
	if err != nil {
		return crypto.ApprovalPublicKeys{}, err
	}
	riskApprovalKey, err := parseRequiredApprovalPublicKey("CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY", cfg.RiskApprovalSigningPublicKey)
	if err != nil {
		return crypto.ApprovalPublicKeys{}, err
	}
	keys := crypto.ApprovalPublicKeys{
		PolicyPublicKey:        policyKey,
		IntentPublicKey:        intentKey,
		RiskPublicKey:          riskKey,
		DecisionPublicKey:      decisionKey,
		AdminApprovalPublicKey: adminKey,
		RiskApprovalPublicKey:  riskApprovalKey,
	}
	if err := crypto.ValidateApprovalPublicKeys(keys); err != nil {
		return crypto.ApprovalPublicKeys{}, err
	}
	return keys, nil
}

func parseRequiredApprovalPublicKey(envName, raw string) (ed25519.PublicKey, error) {
	if raw == "" {
		return nil, fmt.Errorf("%s is required", envName)
	}
	key, err := crypto.ParseEd25519PublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envName, err)
	}
	return key, nil
}
