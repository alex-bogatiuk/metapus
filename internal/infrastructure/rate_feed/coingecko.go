package rate_feed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
	er "github.com/alex-bogatiuk/metapus/internal/domain/registers/exchange_rate"
)

const (
	_coingeckoBaseURL = "https://api.coingecko.com/api/v3"
	_httpTimeout      = 15 * time.Second
	_maxBatch         = 250
	_quoteCacheTTL    = 30 * time.Second
)

type cachedCoinQuote struct {
	rate              decimal.Decimal
	providerUpdatedAt time.Time
	receivedAt        time.Time
	expiresAt         time.Time
}

type coinQuoteCache struct {
	mu      sync.Mutex
	entries map[string]cachedCoinQuote
}

type CoinGeckoFetcher struct {
	client  *http.Client
	baseURL string
	now     func() time.Time
	cache   *coinQuoteCache
}

func NewCoinGeckoFetcher() *CoinGeckoFetcher {
	return &CoinGeckoFetcher{
		client:  &http.Client{Timeout: _httpTimeout},
		baseURL: _coingeckoBaseURL,
		now:     func() time.Time { return time.Now().UTC() },
		cache:   &coinQuoteCache{entries: make(map[string]cachedCoinQuote)},
	}
}

func (f *CoinGeckoFetcher) FetchRates(ctx context.Context, mappings []er.Mapping, rpm int, apiKey string) er.FetchResult {
	result := er.FetchResult{Observations: make([]er.Observation, 0, len(mappings)), Problems: []er.Problem{}, Requested: len(mappings)}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		result.Problems = append(result.Problems, er.Problem{Code: "api_key_missing", Message: "В источнике CoinGecko не настроен ключ API"})
		return result
	}
	lookup := make(map[string][]er.Mapping, len(mappings))
	for _, m := range mappings {
		lookup[m.ExternalID] = append(lookup[m.ExternalID], m)
	}
	now := f.now()
	ids := make([]string, 0, len(lookup))
	if f.cache != nil {
		f.cache.mu.Lock()
		for k := range lookup {
			if cached, ok := f.cache.entries[f.baseURL+"\x00"+k]; ok && now.Before(cached.expiresAt) {
				for _, m := range lookup[k] {
					result.Observations = append(result.Observations, er.Observation{
						Mapping:           m,
						Rate:              cached.rate,
						ProviderUpdatedAt: cached.providerUpdatedAt,
						ReceivedAt:        cached.receivedAt,
					})
				}
				continue
			}
			ids = append(ids, k)
		}
		f.cache.mu.Unlock()
	} else {
		for k := range lookup {
			ids = append(ids, k)
		}
	}
	if len(ids) == 0 {
		return result
	}
	sort.Strings(ids)
	var next time.Time
	for start := 0; start < len(ids); start += _maxBatch {
		if !waitUntil(ctx, next) {
			result.Problems = append(result.Problems, er.Problem{Code: "cancelled", Message: "Получение остановлено"})
			break
		}
		batch := ids[start:min(start+_maxBatch, len(ids))]
		raw, received, retry, err := f.fetchBatch(ctx, batch, apiKey)
		next = time.Now().Add(time.Minute / time.Duration(max(1, rpm)))
		if retry != nil {
			result.RetryAt = retry
		}
		if err != nil {
			for _, coin := range batch {
				for _, m := range lookup[coin] {
					cid := m.Currency.ID
					result.Problems = append(result.Problems, er.Problem{CurrencyID: &cid, Code: "provider_error", Message: err.Error()})
				}
			}
			if retry != nil || ctx.Err() != nil {
				break
			}
			continue
		}
		for _, coin := range batch {
			var prices map[string]json.Number
			pairErr := json.Unmarshal(raw[coin], &prices)
			price, priceErr := decimal.NewFromString(prices["usd"].String())
			stamp, stampErr := prices["last_updated_at"].Int64()
			updated := time.Unix(stamp, 0).UTC()
			code, message := "", ""
			switch {
			case len(raw[coin]) == 0:
				code, message = "pair_missing", "Провайдер не вернул цену"
			case pairErr != nil:
				code, message = "invalid_pair", "Провайдер вернул некорректные данные пары"
			case stampErr != nil || stamp <= 0 || updated.After(received):
				code, message = "invalid_timestamp", "Провайдер вернул некорректное время котировки"
			case priceErr != nil:
				code, message = "invalid_price", "Провайдер вернул некорректную цену"
			default:
				if err := er.ValidatePrice(price, 1); err != nil {
					code, message = "invalid_price", err.Error()
				}
			}
			if code == "" && f.cache != nil {
				f.cache.mu.Lock()
				f.cache.entries[f.baseURL+"\x00"+coin] = cachedCoinQuote{
					rate:              price,
					providerUpdatedAt: updated,
					receivedAt:        received,
					expiresAt:         received.Add(_quoteCacheTTL),
				}
				f.cache.mu.Unlock()
			}
			for _, m := range lookup[coin] {
				if code != "" {
					cid := m.Currency.ID
					result.Problems = append(result.Problems, er.Problem{CurrencyID: &cid, Code: code, Message: message})
					continue
				}
				result.Observations = append(result.Observations, er.Observation{Mapping: m, Rate: price, ProviderUpdatedAt: updated, ReceivedAt: received})
			}
		}
	}
	return result
}
func waitUntil(ctx context.Context, at time.Time) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	delay := time.Until(at)
	if delay <= 0 {
		return true
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func (f *CoinGeckoFetcher) fetchBatch(ctx context.Context, ids []string, apiKey string) (map[string]json.RawMessage, time.Time, *time.Time, error) {
	u, err := url.Parse(f.baseURL + "/simple/price")
	if err != nil {
		return nil, time.Time{}, nil, fmt.Errorf("некорректный адрес провайдера")
	}
	q := u.Query()
	q.Set("ids", strings.Join(ids, ","))
	q.Set("vs_currencies", "usd")
	q.Set("include_last_updated_at", "true")
	q.Set("precision", "full")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, time.Time{}, nil, fmt.Errorf("не удалось создать запрос провайдера")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-cg-demo-api-key", apiKey)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, time.Time{}, nil, fmt.Errorf("не удалось получить ответ CoinGecko")
	}
	defer func() { _ = resp.Body.Close() }()
	received := f.now()
	if resp.StatusCode != http.StatusOK {
		var retry *time.Time
		if resp.StatusCode == http.StatusTooManyRequests {
			t := received.Add(er.FetchInterval)
			if seconds, e := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 32); e == nil && seconds > 0 {
				t = received.Add(time.Duration(seconds) * time.Second)
			} else if parsed, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
				t = parsed
			}
			retry = &t
		}
		return nil, received, retry, fmt.Errorf("CoinGecko: HTTP %d", resp.StatusCode)
	}
	const maxBody = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return nil, received, nil, fmt.Errorf("не удалось прочитать ответ CoinGecko")
	}
	received = f.now()
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(body, &raw); err != nil {
		return nil, received, nil, fmt.Errorf("некорректный ответ CoinGecko")
	}
	return raw, received, nil, nil
}
