package exchange_rate

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestResolveStatesAndTTL(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cfg := Configuration{BaseCurrency: CurrencyRef{ID: id.New(), Code: "USD"}, Source: &SourceRef{ID: id.New()}, SourceActive: true}
	c := Candidate{Currency: CurrencyRef{ID: id.New(), Code: "USDT"}, Mapping: &Mapping{IsActive: true, Version: 2}, Quote: &Quote{ID: id.New(), MappingVersion: 2, ProviderUpdatedAt: at.Add(-899 * time.Second)}}
	r := Resolve(c, cfg, at)
	require.Equal(t, StatusFresh, r.Status)
	require.Same(t, c.Quote, r.SelectedQuote)
	r = Resolve(c, cfg, at.Add(time.Second))
	require.Equal(t, StatusStale, r.Status)
	require.Nil(t, r.SelectedQuote)
	require.Same(t, c.Quote, r.LastKnownQuote)
	c.Mapping.Version++
	require.Equal(t, "mapping_changed", Resolve(c, cfg, at).Reason)
	c.Mapping.IsActive = false
	require.Equal(t, "mapping_disabled", Resolve(c, cfg, at).Reason)
	c.Mapping = nil
	require.Equal(t, "mapping_missing", Resolve(c, cfg, at).Reason)
	cfg.SourceActive = false
	require.Equal(t, "source_disabled", Resolve(c, cfg, at).Reason)
	cfg.Source = nil
	require.Equal(t, "source_not_configured", Resolve(c, cfg, at).Reason)
	c.Currency = cfg.BaseCurrency
	require.Equal(t, StatusIdentity, Resolve(c, cfg, at).Status)
}
func TestExactArithmeticAndRegisterPrecision(t *testing.T) {
	exact := ConvertExact(decimal.RequireFromString("250"), decimal.RequireFromString("0.67"), 100)
	require.Equal(t, "67/40", exact.RatString())
	require.Equal(t, "1.68", RoundAmount(exact, 2))
	exact = ConvertExact(decimal.RequireFromString("-250"), decimal.RequireFromString("0.67"), 100)
	require.Equal(t, "-1.68", RoundAmount(exact, 2))
	require.Equal(t, "0.33", RoundAmount(ConvertExact(decimal.NewFromInt(1), decimal.NewFromInt(1), 3), 2))
	require.Equal(t, "1000000000000000000.00", RoundAmount(ConvertExact(decimal.RequireFromString("1000000000000000000000000000000000000"), decimal.RequireFromString("0.000000000000000001"), 1), 2))
	for _, s := range []string{"0.000000000000000001", "99999999999999999999.999999999999999999", "1.0000000000000000000"} {
		require.NoError(t, ValidatePrice(decimal.RequireFromString(s), 1), s)
	}
	for _, s := range []string{"0", "-1", "0.0000000000000000001", "100000000000000000000", "1.0000000000000000001"} {
		require.Error(t, ValidatePrice(decimal.RequireFromString(s), 1), s)
	}
	require.Error(t, ValidatePrice(decimal.NewFromInt(1), 0))
}

type countingRepo struct {
	currencies []id.ID
	calls      int
	cfg        Configuration
}

func (r *countingRepo) LoadConfiguration(context.Context) (Configuration, error) { return r.cfg, nil }
func (r *countingRepo) Candidates(_ context.Context, _ Configuration, currencies []id.ID, _ time.Time) ([]Candidate, error) {
	r.calls++
	r.currencies = currencies
	return nil, nil
}
func TestResolverDeduplicatesCurrenciesAcrossNetworks(t *testing.T) {
	repo := &countingRepo{}
	service := NewService(repo)
	currency := id.New()
	_, err := service.ResolveBatch(context.Background(), Configuration{}, []id.ID{currency, currency, id.Nil()}, time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, []id.ID{currency}, repo.currencies)
}
