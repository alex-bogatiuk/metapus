package rate_feed

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain/registers/exchange_rate"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

const (
	// _defaultFeedInterval is how often we fetch fresh rates.
	// CoinGecko free tier: 100 calls/min, we call once per 5 minutes.
	_defaultFeedInterval = 5 * time.Minute

	// _jobName is the worker job name for observability.
	_jobName     = "rate_feed.coingecko"
	_jobCategory = "rate_feed"
)

// WorkerConfig holds configuration for the rate feed worker.
type WorkerConfig struct {
	// BaseCurrency is the management reporting currency (e.g., "USD").
	BaseCurrency string

	// Mappings defines CoinGecko coin → Metapus currency relationships.
	// Loaded once at startup. TODO: load dynamically from DB.
	Mappings []CurrencyMapping

	// Interval overrides the default fetch interval.
	// If zero, _defaultFeedInterval is used.
	Interval time.Duration
}

// Worker is a background goroutine that periodically fetches exchange rates
// from CoinGecko and upserts them into reg_exchange_rates.
//
// Lifecycle: Start() blocks until ctx is cancelled.
// Goroutine safety: one owner (the caller), one stop signal (ctx.Done()).
type Worker struct {
	cfg      WorkerConfig
	fetcher  *CoinGeckoFetcher
	rateSvc  *exchange_rate.Service
	recorder *workerjob.Recorder
	log      *logger.Logger
}

// NewWorker creates a new rate feed worker.
// rateSvc is injected (not created internally) for testability and DI consistency.
func NewWorker(cfg WorkerConfig, rateSvc *exchange_rate.Service, recorder *workerjob.Recorder, log *logger.Logger) *Worker {
	fetcher := NewCoinGeckoFetcher(cfg.BaseCurrency, log)

	return &Worker{
		cfg:      cfg,
		fetcher:  fetcher,
		rateSvc:  rateSvc,
		recorder: recorder,
		log:      log.WithComponent("rate_feed"),
	}
}

// Start begins the periodic rate feed loop. Blocks until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	interval := w.cfg.Interval
	if interval == 0 {
		interval = _defaultFeedInterval
	}

	// Immediate first fetch.
	w.fetchAndStore(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("rate feed worker stopped")
			return
		case <-ticker.C:
			w.fetchAndStore(ctx)
		}
	}
}

func (w *Worker) fetchAndStore(ctx context.Context) {
	if w.recorder != nil {
		w.recorder.RecordIfWork(ctx, _jobName, _jobCategory, func(ctx context.Context) (int, error) {
			return w.doFetch(ctx)
		})
	} else {
		if _, err := w.doFetch(ctx); err != nil {
			w.log.Errorw("rate feed fetch failed", "error", err)
		}
	}
}

func (w *Worker) doFetch(ctx context.Context) (int, error) {
	if len(w.cfg.Mappings) == 0 {
		return 0, nil
	}

	rates, err := w.fetcher.FetchRates(ctx, w.cfg.Mappings)
	if err != nil {
		return 0, fmt.Errorf("fetch rates: %w", err)
	}

	if len(rates) == 0 {
		return 0, nil
	}

	// Upsert each rate inside the tenant's tx context.
	// ExchangeRate.Upsert uses ON CONFLICT — no transaction needed for atomicity.
	stored := 0
	for i := range rates {
		if err := w.rateSvc.UpsertRate(ctx, &rates[i]); err != nil {
			w.log.Warnw("failed to upsert rate",
				"currency_id", rates[i].CurrencyID,
				"error", err,
			)
			continue
		}
		stored++
	}

	w.log.Infow("exchange rates updated",
		"source", "coingecko",
		"fetched", len(rates),
		"stored", stored,
	)

	return stored, nil
}

// BuildMappingsFromDB loads currency mappings from the rate source mappings register.
// Queries reg_rate_source_mappings JOIN cat_rate_sources JOIN cat_currencies
// for the given sourceType (e.g. "coingecko").
// Uses minor_multiplier from cat_currencies as the rate multiplier (authoritative source).
func BuildMappingsFromDB(ctx context.Context, sourceType string) ([]CurrencyMapping, error) {
	txm := postgres.MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	const sql = `
		SELECT m.external_id, c.id, c.minor_multiplier, rs.id
		FROM reg_rate_source_mappings m
		JOIN cat_rate_sources rs ON rs.id = m.rate_source_id
		JOIN cat_currencies c ON c.id = m.currency_id
		WHERE rs.source_type = $1
		  AND rs._deleted_at IS NULL
		  AND m.is_active = TRUE
		  AND c._deleted_at IS NULL
	`

	rows, err := q.Query(ctx, sql, sourceType)
	if err != nil {
		return nil, fmt.Errorf("query rate source mappings: %w", err)
	}
	defer rows.Close()

	var mappings []CurrencyMapping
	for rows.Next() {
		var (
			externalID      string
			currencyID      id.ID
			minorMultiplier int64
			rateSourceID    id.ID
		)
		if err := rows.Scan(&externalID, &currencyID, &minorMultiplier, &rateSourceID); err != nil {
			return nil, fmt.Errorf("scan rate source mapping: %w", err)
		}

		multiplier := 1
		if minorMultiplier >= 100 {
			multiplier = int(minorMultiplier)
		}

		mappings = append(mappings, CurrencyMapping{
			CoinGeckoID:  externalID,
			CurrencyID:   currencyID,
			RateSourceID: rateSourceID,
			Multiplier:   multiplier,
		})
	}

	return mappings, rows.Err()
}
