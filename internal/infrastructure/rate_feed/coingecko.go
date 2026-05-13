// Package rate_feed provides background rate feed workers for populating
// the reg_exchange_rates register from external sources (CoinGecko, etc.).
package rate_feed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/id"
	"metapus/internal/domain/registers/exchange_rate"
	"metapus/pkg/logger"
)

const (
	// _coingeckoBaseURL is the CoinGecko free API base URL.
	_coingeckoBaseURL = "https://api.coingecko.com/api/v3"

	// _httpTimeout is the maximum duration for a single CoinGecko API call.
	_httpTimeout = 15 * time.Second
)

// CurrencyMapping maps a CoinGecko coin ID to a Metapus currency UUID.
// Example: {"tether": currencyID_of_USDT, "bitcoin": currencyID_of_BTC}.
type CurrencyMapping struct {
	CoinGeckoID  string // CoinGecko coin identifier (e.g., "tether", "bitcoin")
	CurrencyID   id.ID  // Metapus cat_currencies.id
	RateSourceID id.ID  // Metapus cat_rate_sources.id
	Multiplier   int    // Rate multiplier (1 for most, 100 for JPY-like)
}

// CoinGeckoFetcher fetches exchange rates from CoinGecko /simple/price API.
// Thread-safe: stateless client, can be called from multiple goroutines.
type CoinGeckoFetcher struct {
	client       *http.Client
	baseCurrency string // "usd"
	log          *logger.Logger
}

// NewCoinGeckoFetcher creates a new CoinGecko rate fetcher.
func NewCoinGeckoFetcher(baseCurrency string, log *logger.Logger) *CoinGeckoFetcher {
	return &CoinGeckoFetcher{
		client: &http.Client{
			Timeout: _httpTimeout,
		},
		baseCurrency: strings.ToLower(baseCurrency),
		log:          log.WithComponent("coingecko"),
	}
}

// FetchRates fetches current prices for the given coins from CoinGecko
// and converts them to ExchangeRate records ready for upsert.
func (f *CoinGeckoFetcher) FetchRates(ctx context.Context, mappings []CurrencyMapping) ([]exchange_rate.ExchangeRate, error) {
	if len(mappings) == 0 {
		return nil, nil
	}

	// Build comma-separated coin IDs.
	coinIDs := make([]string, 0, len(mappings))
	for _, m := range mappings {
		coinIDs = append(coinIDs, m.CoinGeckoID)
	}
	ids := strings.Join(coinIDs, ",")

	u, err := url.Parse(_coingeckoBaseURL + "/simple/price")
	if err != nil {
		return nil, fmt.Errorf("parse CoinGecko URL: %w", err)
	}
	q := u.Query()
	q.Set("ids", ids)
	q.Set("vs_currencies", f.baseCurrency)
	q.Set("include_last_updated_at", "true")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CoinGecko request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("CoinGecko HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Parse response: {"tether": {"usd": 0.9997, "last_updated_at": 1715600000}, ...}
	var raw map[string]map[string]json.Number
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode CoinGecko response: %w", err)
	}

	// Build lookup: CoinGecko ID → mapping
	lookup := make(map[string]CurrencyMapping, len(mappings))
	for _, m := range mappings {
		lookup[m.CoinGeckoID] = m
	}

	now := time.Now().UTC().Truncate(24 * time.Hour) // daily granularity
	rates := make([]exchange_rate.ExchangeRate, 0, len(mappings))

	for coinID, prices := range raw {
		mapping, ok := lookup[coinID]
		if !ok {
			continue
		}

		priceStr, ok := prices[f.baseCurrency]
		if !ok {
			f.log.Warnw("no price for base currency",
				"coin_id", coinID,
				"base", f.baseCurrency,
			)
			continue
		}

		rate, err := decimal.NewFromString(priceStr.String())
		if err != nil {
			f.log.Warnw("invalid price value",
				"coin_id", coinID,
				"value", priceStr.String(),
				"error", err,
			)
			continue
		}

		multiplier := mapping.Multiplier
		if multiplier < 1 {
			multiplier = 1
		}

		rates = append(rates, exchange_rate.ExchangeRate{
			CurrencyID:   mapping.CurrencyID,
			Date:         now,
			Rate:         rate,
			Multiplier:   multiplier,
			RateSourceID: mapping.RateSourceID,
		})
	}

	return rates, nil
}
