package rate_feed

import (
	"context"
	"fmt"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	er "github.com/alex-bogatiuk/metapus/internal/domain/registers/exchange_rate"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mapping(coin string) er.Mapping {
	return er.Mapping{Currency: er.CurrencyRef{ID: id.New()}, ExternalID: coin, IsActive: true, Version: 1}
}
func TestCoinGeckoExactPartialResponse(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-cg-demo-api-key") != "local-test" || r.URL.Query().Get("precision") != "full" || r.URL.Query().Get("include_last_updated_at") != "true" || r.URL.Query().Get("vs_currencies") != "usd" {
			t.Error("invalid provider contract")
		}
		_, _ = fmt.Fprintf(w, `{"ok":{"usd":0.123456789123456789,"last_updated_at":%d},"future":{"usd":1,"last_updated_at":%d},"tiny":{"usd":0.0000000000000000001,"last_updated_at":%d},"nostamp":{"usd":1},"zero":{"usd":0,"last_updated_at":%d}}`, at.Unix(), at.Add(time.Hour).Unix(), at.Unix(), at.Unix())
	}))
	defer server.Close()
	f := NewCoinGeckoFetcher()
	f.baseURL = server.URL
	result := f.FetchRates(context.Background(), []er.Mapping{mapping("ok"), mapping("ok"), mapping("future"), mapping("tiny"), mapping("nostamp"), mapping("zero"), mapping("absent")}, 1000, "local-test")
	require.Equal(t, 7, result.Requested)
	require.Len(t, result.Observations, 2)
	require.Len(t, result.Problems, 5)
	require.Equal(t, "0.123456789123456789", result.Observations[0].Rate.String())
}
func TestCoinGeckoBatchLimitAndFanout(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		coins := strings.Split(r.URL.Query().Get("ids"), ",")
		if len(coins) > 250 {
			t.Error("batch exceeds 250")
		}
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	f := NewCoinGeckoFetcher()
	f.baseURL = server.URL
	var mappings []er.Mapping
	for i := range 251 {
		mappings = append(mappings, mapping(fmt.Sprintf("coin-%d", i)))
	}
	mappings = append(mappings, mappings[0])
	result := f.FetchRates(context.Background(), mappings, 60000, "test")
	require.Equal(t, int32(2), calls.Load())
	require.Equal(t, 252, result.Requested)
	require.Empty(t, result.Observations)
	require.Len(t, result.Problems, 252)
}
func TestCoinGeckoRetryTimeoutAndConfiguration(t *testing.T) {
	f := NewCoinGeckoFetcher()
	result := f.FetchRates(context.Background(), []er.Mapping{mapping("tether")}, 30, "")
	require.Equal(t, "api_key_missing", result.Problems[0].Code)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1800")
		w.WriteHeader(429)
	}))
	defer server.Close()
	f = NewCoinGeckoFetcher()
	f.baseURL = server.URL
	result = f.FetchRates(context.Background(), []er.Mapping{mapping("tether")}, 30, "test")
	require.NotNil(t, result.RetryAt)
	require.True(t, result.RetryAt.After(time.Now().Add(29*time.Minute)))
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	f.baseURL = slow.URL
	f.client.Timeout = 20 * time.Millisecond
	result = f.FetchRates(context.Background(), []er.Mapping{mapping("tether")}, 30, "test")
	require.Empty(t, result.Observations)
	require.Len(t, result.Problems, 1)
	require.Equal(t, 15*time.Second, NewCoinGeckoFetcher().client.Timeout)
}
