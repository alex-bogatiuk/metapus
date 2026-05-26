package tron

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/pkg/logger"
)

const (
	_defaultPollInterval   = 3 * time.Second  // TRON block time ~3s
	_maxPollInterval       = 30 * time.Second // backoff ceiling
	_confirmationPollDelay = 5 * time.Second  // delay between confirmation checks
)

// WatcherConfig holds configuration for the TRON chain watcher.
type WatcherConfig struct {
	// NetworkID is the resolved BlockchainNetwork UUID.
	NetworkID id.ID

	// ContractAddress is the TRC-20 token contract to monitor (e.g., USDT).
	ContractAddress string

	// PollInterval is the base polling interval (default 3s).
	PollInterval time.Duration

	// RequiredConfirmations for this network.
	RequiredConfirmations int

	// MonitoredAddresses is the set of wallet addresses to watch.
	// Updated dynamically when wallets are created/released.
	MonitoredAddresses map[string]bool
}

// WatcherState represents persisted checkpoint state.
type WatcherState struct {
	NetworkID     id.ID     `db:"network_id" json:"networkId"`
	LastBlock     int64     `db:"last_block" json:"lastBlock"`
	LastTimestamp int64     `db:"last_timestamp" json:"lastTimestamp"`
	Fingerprint   string    `db:"fingerprint" json:"fingerprint"`
	UpdatedAt     time.Time `db:"updated_at" json:"updatedAt"`
}

// WatcherStateRepository persists chain watcher checkpoints.
type WatcherStateRepository interface {
	Get(ctx context.Context, networkID id.ID) (*WatcherState, error)
	Save(ctx context.Context, state *WatcherState) error
}

// Watcher implements crypto.ChainWatcher for TRON network.
// Polls TronGrid API for TRC-20 Transfer events and emits BlockchainEvent.
type Watcher struct {
	client    *Client
	cfg       WatcherConfig
	stateRepo WatcherStateRepository
}

// NewWatcher creates a new TRON chain watcher.
func NewWatcher(client *Client, cfg WatcherConfig, stateRepo WatcherStateRepository) *Watcher {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = _defaultPollInterval
	}
	if cfg.RequiredConfirmations == 0 {
		cfg.RequiredConfirmations = 20
	}

	return &Watcher{
		client:    client,
		cfg:       cfg,
		stateRepo: stateRepo,
	}
}

// NetworkCode implements crypto.ChainWatcher.
func (w *Watcher) NetworkCode() string { return "tron_mainnet" }

// Start implements crypto.ChainWatcher.
// Polls for TRC-20 Transfer events and emits normalized BlockchainEvent.
// Blocks until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context, addresses []string, events chan<- crypto.BlockchainEvent) error {
	// Populate monitored addresses
	w.cfg.MonitoredAddresses = make(map[string]bool, len(addresses))
	for _, addr := range addresses {
		w.cfg.MonitoredAddresses[addr] = true
	}

	// Load checkpoint
	state, err := w.stateRepo.Get(ctx, w.cfg.NetworkID)
	if err != nil {
		// No checkpoint — start from current time
		state = &WatcherState{
			NetworkID:     w.cfg.NetworkID,
			LastTimestamp: time.Now().Add(-15 * time.Minute).UnixMilli(),
			UpdatedAt:     time.Now().UTC(),
		}
	}

	pollInterval := w.cfg.PollInterval
	consecutiveErrors := 0

	logger.Info(ctx, "TRON watcher started",
		"network_id", w.cfg.NetworkID,
		"contract", w.cfg.ContractAddress,
		"addresses", len(w.cfg.MonitoredAddresses),
		"last_block", state.LastBlock,
	)

	// Use Timer instead of time.After to prevent GC leak on each loop iteration.
	pollTimer := time.NewTimer(pollInterval)
	defer pollTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "TRON watcher stopping",
				"network_id", w.cfg.NetworkID,
				"last_block", state.LastBlock,
			)
			return ctx.Err()

		case <-pollTimer.C:
			eventsFound, err := w.poll(ctx, state, events)
			if err != nil {
				consecutiveErrors++
				pollInterval = w.backoff(pollInterval, consecutiveErrors)
				logger.Error(ctx, "TRON poll failed",
					"error", err,
					"consecutive_errors", consecutiveErrors,
					"next_poll", pollInterval,
				)
				pollTimer.Reset(pollInterval)
				continue
			}

			consecutiveErrors = 0

			// Adaptive polling: speed up if events found or in catch-up mode, slow down if idle
			now := time.Now().UnixMilli()
			catchUpThreshold := now - int64(time.Minute.Milliseconds())

			if state.LastTimestamp < catchUpThreshold {
				// Catch-up mode: turbo polling to catch up with the chain
				pollInterval = 500 * time.Millisecond
			} else if eventsFound > 0 {
				pollInterval = w.cfg.PollInterval // reset to base
			} else {
				// Gradually slow down (but don't exceed max)
				pollInterval = min(time.Duration(float64(pollInterval)*1.2), _maxPollInterval)
			}

			// Save checkpoint
			state.UpdatedAt = time.Now().UTC()
			if err := w.stateRepo.Save(ctx, state); err != nil {
				logger.Error(ctx, "failed to save watcher checkpoint",
					"error", err,
				)
			}

			pollTimer.Reset(pollInterval)
		}
	}
}

// GetConfirmations implements crypto.ChainWatcher.
func (w *Watcher) GetConfirmations(ctx context.Context, txHash string) (int, error) {
	return w.client.GetConfirmations(ctx, txHash)
}

// poll fetches new events since the last checkpoint.
// TronGrid fingerprints are pagination cursors valid only within a single query
// (same params). They must NOT be persisted across polls — only used for
// paginating through multiple pages within one poll() call.
func (w *Watcher) poll(ctx context.Context, state *WatcherState, events chan<- crypto.BlockchainEvent) (int, error) {
	eventsFound := 0
	fingerprint := "" // ephemeral — used only for intra-poll pagination

	now := time.Now().UnixMilli()
	maxWindowMs := int64(time.Hour.Milliseconds())
	maxTimestamp := min(state.LastTimestamp+maxWindowMs, now)

	for {
		resp, err := w.client.GetTRC20Events(ctx, w.cfg.ContractAddress, state.LastTimestamp, maxTimestamp, fingerprint)
		if err != nil {
			if IsFingerprintError(err) {
				logger.Warn(ctx, "TronGrid fingerprint expired during pagination, will restart from last checkpoint",
					"last_timestamp", state.LastTimestamp,
					"events_found_so_far", eventsFound,
				)
				// Returning nil error allows the watcher to save the current progress (updated state)
				// and cleanly start a fresh poll without the stale fingerprint on the next tick.
				return eventsFound, nil
			}
			return eventsFound, fmt.Errorf("fetch events: %w", err)
		}

		if !resp.Success {
			return eventsFound, fmt.Errorf("TronGrid API returned success=false")
		}

		for _, event := range resp.Data {
			// Convert hex address from TronGrid events API → base58 for matching
			toAddr := ConvertTronAddress(event.Result.To)
			if !w.cfg.MonitoredAddresses[toAddr] {
				continue
			}

			// Skip events we've already processed (idempotency by block number)
			if event.BlockNumber <= state.LastBlock && state.LastBlock > 0 {
				continue
			}

			// Convert to normalized event
			blockchainEvent := w.client.ToBlockchainEvent(event, w.cfg.NetworkID)

			// Fetch confirmation count
			confs, err := w.client.GetConfirmations(ctx, event.TransactionID)
			if err != nil {
				logger.Warn(ctx, "failed to get confirmations, defaulting to 0",
					"tx_hash", event.TransactionID,
					"error", err,
				)
			} else {
				blockchainEvent.Confirmations = confs
			}

			blockchainEvent.TokenContract = w.cfg.ContractAddress
			blockchainEvent.RequiredConfs = w.cfg.RequiredConfirmations

			// Emit event
			select {
			case events <- blockchainEvent:
				eventsFound++
			case <-ctx.Done():
				return eventsFound, ctx.Err()
			}
		}

		// Update checkpoint from last event in this page
		if len(resp.Data) > 0 {
			lastEvent := resp.Data[len(resp.Data)-1]
			state.LastBlock = lastEvent.BlockNumber
			state.LastTimestamp = lastEvent.BlockTimestamp
		}

		// Continue pagination if more pages available
		if resp.Meta.Fingerprint != "" {
			fingerprint = resp.Meta.Fingerprint
			continue
		}

		// No more pages — window complete.
		// Advance LastTimestamp to the end of the window to ensure we move forward
		// even if there were no events (or the last event was earlier in the window).
		if state.LastTimestamp < maxTimestamp {
			state.LastTimestamp = maxTimestamp
		}

		return eventsFound, nil
	}
}

// backoff calculates the next poll interval with exponential backoff.
func (w *Watcher) backoff(current time.Duration, errorCount int) time.Duration {
	next := min(current*2, _maxPollInterval)
	return next
}

// Compile-time check: Watcher implements crypto.ChainWatcher.
var _ crypto.ChainWatcher = (*Watcher)(nil)
