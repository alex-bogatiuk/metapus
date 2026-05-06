// Package crypto_worker provides the background crypto processing integration.
// It bridges ChainWatcher (TRON) → EventProcessor within the Worker lifecycle.
//
// The CryptoProcessor is started per-tenant alongside the outbox relay
// and automation scheduler. It manages:
//   - Loading wallet addresses from DB
//   - Starting ChainWatcher goroutines per blockchain network
//   - Consuming BlockchainEvent from watchers → EventProcessor
//   - Invoice expiration ticker (marks expired invoices)
//   - Periodic wallet address refresh (picks up new wallets)
package crypto_worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/blockchain/tron"
	infraNumerator "metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/pkg/logger"
)

const (
	_eventChannelBuffer      = 100
	_addressRefreshPeriod    = 5 * time.Minute
	_expirationCheckPeriod   = 1 * time.Minute
	_confirmationPollPeriod  = 10 * time.Second
	_sweepEvalPeriod         = 1 * time.Minute
	_sweepEvalBatchSize      = 500
)

// CryptoProcessorConfig holds configuration for the crypto processor.
type CryptoProcessorConfig struct {
	// TRONRpcURL is the TronGrid API endpoint (e.g., https://api.shasta.trongrid.io).
	// If empty, TRON watcher is not started.
	TRONRpcURL string

	// TRONApiKey is the optional TronGrid API key for higher rate limits.
	TRONApiKey string
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
	stateRepo      tron.WatcherStateRepository

	// Domain
	eventProcessor *crypto.EventProcessor
	walletSvc      *wallet.Service
	sweepResolver  *crypto.SweepConfigResolver

	// Chain watchers per network (for confirmation re-checks)
	mu             sync.Mutex
	chainWatchers  map[id.ID]crypto.ChainWatcher
}

// NewCryptoProcessor creates a new crypto processor.
func NewCryptoProcessor(cfg CryptoProcessorConfig, log *logger.Logger) *CryptoProcessor {
	walletRepo := catalog_repo.NewWalletRepo()
	invoiceRepo := document_repo.NewCryptoInvoiceRepo()
	paymentRepo := document_repo.NewCryptoPaymentRepo()
	stateRepo := crypto_repo.NewWatcherStateRepo()

	// Build posting engine (for auto-posting confirmed payments)
	// DocLocker is not needed for worker-driven posting — we control the flow.
	postingEngine := posting.NewEngine(nil) // nil locker: worker doesn't use optimistic lock

	// Wallet service (for FindByAddress, MarkSweepPending)
	// Numerator is nil-safe: worker doesn't create wallets.
	walletSvc := wallet.NewService(walletRepo, numerator.Noop())

	// Payment FSM
	eventRepo := crypto_repo.NewPaymentEventRepo()
	fsm := crypto.NewPaymentFSM(paymentRepo, eventRepo)

	// Sweep config resolver (token defaults + merchant overrides)
	tokenRepo := catalog_repo.NewTokenRepo()
	merchantTokenCfgRepo := crypto_repo.NewMerchantTokenConfigRepo()
	sweepResolver := crypto.NewSweepConfigResolver(merchantTokenCfgRepo, tokenRepo)

	// Event Processor — TxManager is extracted from context at runtime.
	// Numerator generates sequential document numbers (CP-2026-00001).
	num := infraNumerator.New()
	ep := crypto.NewEventProcessor(crypto.EventProcessorConfig{
		FSM:           fsm,
		WalletSvc:     walletSvc,
		InvoiceRepo:   invoiceRepo,
		PaymentRepo:   paymentRepo,
		PostingEngine: postingEngine,
		TxManager:     contextTxManager{},
		Numerator:     num,
		SweepResolver: sweepResolver,
	})

	return &CryptoProcessor{
		cfg:            cfg,
		log:            log.WithComponent("crypto"),
		walletRepo:     walletRepo,
		invoiceRepo:    invoiceRepo,
		paymentRepo:    paymentRepo,
		stateRepo:      stateRepo,
		eventProcessor: ep,
		walletSvc:      walletSvc,
		sweepResolver:  sweepResolver,
		chainWatchers:  make(map[id.ID]crypto.ChainWatcher),
	}
}

// Start begins crypto processing. Blocks until ctx is cancelled.
// Starts per-network watchers + event consumer + maintenance tickers.
func (p *CryptoProcessor) Start(ctx context.Context) {
	// Load blockchain networks to determine which watchers to start
	networks, err := p.loadNetworks(ctx)
	if err != nil {
		p.log.Errorw("failed to load blockchain networks, crypto processing disabled",
			"error", err,
		)
		return
	}

	if len(networks) == 0 {
		p.log.Info("no active blockchain networks found, crypto processing idle")
		// Still run expiration ticker
		p.runExpirationLoop(ctx)
		return
	}

	events := make(chan crypto.BlockchainEvent, _eventChannelBuffer)

	// Separate WaitGroup for watchers: when all watchers finish, close the channel.
	// This unblocks the consumer goroutine cleanly (C1).
	var watcherWg sync.WaitGroup
	var consumerWg sync.WaitGroup

	// Start chain watchers
	for _, net := range networks {
		switch {
		case net.code == "TRON-SHASTA" || net.code == "TRON-MAINNET":
			if p.cfg.TRONRpcURL == "" {
				p.log.Warnw("TRON RPC URL not configured, skipping TRON watcher",
					"network", net.code,
				)
				continue
			}
			watcherWg.Add(1)
			go func(net networkInfo) {
				defer watcherWg.Done()
				p.runTRONWatcher(ctx, net, events)
			}(net)
		default:
			p.log.Warnw("unsupported blockchain network, skipping",
				"network", net.code,
				"chain_id", net.chainID,
			)
		}
	}

	// Close events channel after all watchers stop — unblocks consumer.
	go func() {
		watcherWg.Wait()
		close(events)
	}()

	// Start event consumer
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		p.consumeEvents(ctx, events)
	}()

	// Start expiration ticker
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		p.runExpirationLoop(ctx)
	}()

	// Start confirmation poll loop (re-checks confirming payments)
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		p.runConfirmationLoop(ctx)
	}()

	// Start sweep evaluation loop (checks accumulated balances against thresholds)
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		p.runSweepEvaluationLoop(ctx)
	}()

	p.log.Infow("crypto processor started",
		"networks", len(networks),
	)

	// Wait for consumer + expiration to finish
	consumerWg.Wait()

	p.log.Info("crypto processor stopped")
}

// runTRONWatcher starts a TRON chain watcher for a specific network.
func (p *CryptoProcessor) runTRONWatcher(ctx context.Context, net networkInfo, events chan<- crypto.BlockchainEvent) {
	// Load wallet addresses for this network
	addresses, err := p.loadWalletAddresses(ctx, net.id)
	if err != nil {
		p.log.Errorw("failed to load wallet addresses",
			"network", net.code,
			"error", err,
		)
		return
	}

	if len(addresses) == 0 {
		p.log.Warnw("no wallets found for network, watcher will poll for new wallets",
			"network", net.code,
		)
	}

	client := tron.NewClient(tron.ClientConfig{
		BaseURL: p.cfg.TRONRpcURL,
		APIKey:  p.cfg.TRONApiKey,
	})
	watcher := tron.NewWatcher(client, tron.WatcherConfig{
		NetworkID:             net.id,
		ContractAddress:       net.tokenContract,
		RequiredConfirmations: net.requiredConfs,
	}, p.stateRepo)

	// Register watcher for confirmation re-checks
	p.mu.Lock()
	p.chainWatchers[net.id] = watcher
	p.mu.Unlock()

	p.log.Infow("starting TRON watcher",
		"network", net.code,
		"addresses", len(addresses),
		"contract", net.tokenContract,
	)

	if err := watcher.Start(ctx, addresses, events); err != nil && ctx.Err() == nil {
		p.log.Errorw("TRON watcher stopped with error",
			"network", net.code,
			"error", err,
		)
	}
}

// consumeEvents reads BlockchainEvents from all watchers and processes them.
func (p *CryptoProcessor) consumeEvents(ctx context.Context, events <-chan crypto.BlockchainEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}

			if err := p.eventProcessor.ProcessEvent(ctx, event); err != nil {
				p.log.Errorw("failed to process blockchain event",
					"tx_hash", event.TxHash,
					"network_id", event.NetworkID,
					"error", err,
				)
				// Continue processing — one failed event shouldn't stop the processor
			}
		}
	}
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
			if err := p.expireInvoices(ctx); err != nil {
				p.log.Errorw("failed to expire invoices", "error", err)
			}
		}
	}
}

// expireInvoices marks invoices that have passed their expiration time.
// Delegates to repository layer to maintain Clean Architecture boundary.
func (p *CryptoProcessor) expireInvoices(ctx context.Context) error {
	count, err := p.invoiceRepo.ExpireOverdue(ctx)
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

// runConfirmationLoop periodically re-checks confirmations for payments in "confirming" status.
// When a payment reaches RequiredConfs, EventProcessor drives it to "confirmed" + posting + sweep.
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

// pollConfirmations loads all "confirming" payments and re-checks their on-chain confirmation count.
func (p *CryptoProcessor) pollConfirmations(ctx context.Context) error {
	payments, err := p.paymentRepo.ListByStatus(ctx, crypto_payment.PaymentStatusConfirming)
	if err != nil {
		return fmt.Errorf("list confirming payments: %w", err)
	}

	if len(payments) == 0 {
		return nil
	}

	p.log.Debugw("confirmation poll: found confirming payments",
		"count", len(payments),
	)

	// Snapshot watchers under lock
	p.mu.Lock()
	watchers := make([]struct {
		networkID id.ID
		watcher   crypto.ChainWatcher
	}, 0, len(p.chainWatchers))
	for netID, w := range p.chainWatchers {
		watchers = append(watchers, struct {
			networkID id.ID
			watcher   crypto.ChainWatcher
		}{netID, w})
	}
	p.mu.Unlock()

	for _, payment := range payments {
		var confs int
		var matchedNetworkID id.ID

		// Try each chain watcher (typically just one) to get confirmations
		for _, w := range watchers {
			c, err := w.watcher.GetConfirmations(ctx, payment.TxHash)
			if err != nil {
				continue // not on this network
			}
			if c > 0 {
				confs = c
				matchedNetworkID = w.networkID
				break
			}
		}

		// Only process if confirmations increased
		if confs <= payment.Confirmations {
			continue
		}

		// Feed a synthetic event to EventProcessor — it handles idempotency via FindByTxHash
		event := crypto.BlockchainEvent{
			NetworkID:     matchedNetworkID,
			TxHash:        payment.TxHash,
			Amount:        payment.Amount,
			BlockNumber:   payment.BlockNumber,
			Confirmations: confs,
			RequiredConfs: payment.RequiredConfs,
			EventType:     crypto.EventTypeTransfer,
		}

		if err := p.eventProcessor.ProcessEvent(ctx, event); err != nil {
			p.log.Errorw("failed to process confirmation update",
				"payment_id", payment.ID,
				"tx_hash", payment.TxHash,
				"confirmations", confs,
				"error", err,
			)
		}
	}

	return nil
}

// ── Sweep Evaluation ────────────────────────────────────────────────────

// runSweepEvaluationLoop periodically checks pool wallets for threshold-based sweep.
// Wallets with accumulated balance ≥ threshold (or exceeding max age) are marked sweep_pending.
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
			if err := p.evaluateSweeps(ctx); err != nil {
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
	balance         types.CryptoAmount
	lastSweptAt     *time.Time
	oldestPaymentAt *time.Time // MIN(confirmed_at) — for max-age check on never-swept wallets
}

// evaluateSweeps queries pool wallets and checks if any should be swept.
func (p *CryptoProcessor) evaluateSweeps(ctx context.Context) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	// Query pool wallets with accumulated confirmed payments since last sweep.
	// Only wallets NOT already in sweep_pending or frozen.
	// Groups by wallet to sum all confirmed payments since last_swept_at.
	const query = `
		SELECT w.id AS wallet_id,
		       w.merchant_id,
		       w.last_swept_at,
		       p.token_id,
		       COALESCE(SUM(p.amount), 0) AS balance,
		       MIN(p.confirmed_at)        AS oldest_payment_at
		FROM cat_wallets w
		INNER JOIN doc_crypto_payments p ON p.wallet_id = w.id
		WHERE w.tier = 'pool'
		  AND w.status IN ('free', 'assigned')
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
		  AND p.status = 'confirmed'
		  AND p.confirmed_at > COALESCE(w.last_swept_at, '1970-01-01'::timestamptz)
		GROUP BY w.id, w.merchant_id, w.last_swept_at, p.token_id
		HAVING COALESCE(SUM(p.amount), 0) > 0
		LIMIT $1
	`

	rows, err := querier.Query(ctx, query, _sweepEvalBatchSize)
	if err != nil {
		return fmt.Errorf("query sweep candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]sweepCandidate, 0, _sweepEvalBatchSize)
	for rows.Next() {
		var c sweepCandidate
		if err := rows.Scan(&c.walletID, &c.merchantID, &c.lastSweptAt, &c.tokenID, &c.balance, &c.oldestPaymentAt); err != nil {
			return fmt.Errorf("scan sweep candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sweep candidates: %w", err)
	}

	if len(candidates) == 0 {
		return nil
	}

	p.log.Infow("evaluating sweep candidates", "count", len(candidates))

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

		// Check threshold
		thresholdMet := c.balance.Cmp(cfg.Threshold) >= 0

		// Check max age (if configured)
		ageMet := false
		if cfg.MaxAgeHours > 0 {
			maxAge := time.Duration(cfg.MaxAgeHours) * time.Hour
			if c.lastSweptAt != nil {
				ageMet = time.Since(*c.lastSweptAt) > maxAge
			} else if c.oldestPaymentAt != nil {
				// Never swept: check age from oldest unswept confirmed payment.
				// Previously this was always-true, causing premature sweep
				// before threshold was reached.
				ageMet = time.Since(*c.oldestPaymentAt) > maxAge
			}
		}

		if thresholdMet || ageMet {
			p.log.Infow("marking wallet for sweep",
				"wallet_id", c.walletID,
				"balance", c.balance.String(),
				"threshold", cfg.Threshold.String(),
				"threshold_met", thresholdMet,
				"age_met", ageMet,
			)

			if err := p.walletSvc.MarkSweepPending(ctx, c.walletID); err != nil {
				p.log.Errorw("failed to mark wallet sweep pending",
					"wallet_id", c.walletID,
					"error", err,
				)
			}
		}
	}

	return nil
}

// ── Internal helpers ────────────────────────────────────────────────────

type networkInfo struct {
	id            id.ID
	code          string
	chainID       string
	tokenContract string
	requiredConfs int
}

// loadNetworks loads active blockchain networks + their primary token contract.
// Uses DISTINCT ON to prevent duplicate networks when multiple tokens are active (B5).
func (p *CryptoProcessor) loadNetworks(ctx context.Context) ([]networkInfo, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		SELECT DISTINCT ON (n.id)
		       n.id, n.code, n.chain_id, 
		       COALESCE(t.contract_address, '') AS token_contract,
		       n.confirmations_needed
		FROM cat_blockchain_networks n
		LEFT JOIN cat_tokens t ON t.network_id = n.id AND t.is_active = true
		WHERE n.is_active = true
		ORDER BY n.id, t.code ASC
		LIMIT 20
	`)
	if err != nil {
		return nil, fmt.Errorf("query networks: %w", err)
	}
	defer rows.Close()

	var networks []networkInfo
	for rows.Next() {
		var net networkInfo
		if err := rows.Scan(&net.id, &net.code, &net.chainID, &net.tokenContract, &net.requiredConfs); err != nil {
			return nil, fmt.Errorf("scan network: %w", err)
		}
		if net.tokenContract != "" {
			networks = append(networks, net)
		}
	}

	return networks, rows.Err()
}

// loadWalletAddresses loads all pool wallet addresses for a network.
func (p *CryptoProcessor) loadWalletAddresses(ctx context.Context, networkID id.ID) ([]string, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		SELECT address FROM cat_wallets 
		WHERE network_id = $1 
		  AND tier = $2
		ORDER BY code
	`, networkID, wallet.WalletTierPool)
	if err != nil {
		return nil, fmt.Errorf("query wallet addresses: %w", err)
	}
	defer rows.Close()

	var addresses []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, fmt.Errorf("scan address: %w", err)
		}
		addresses = append(addresses, addr)
	}

	return addresses, rows.Err()
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

