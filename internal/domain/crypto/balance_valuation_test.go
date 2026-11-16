package crypto

import (
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	er "metapus/internal/domain/registers/exchange_rate"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestValuationCoverageAndPendingOnlyPositions(t *testing.T) {
	usd, missing := id.New(), id.New()
	cfg := er.Configuration{BaseCurrency: er.CurrencyRef{ID: usd, Code: "USD", DecimalPlaces: 2}}
	identity := er.Resolution{Status: er.StatusIdentity}
	pendingToken := id.New()
	rows := []BalanceRow{{TokenID: id.New(), CurrencyID: &usd, RawAmount: types.NewRawAmountFromInt64(7)}, {TokenID: pendingToken, CurrencyID: &missing, RawAmount: types.ZeroRawAmount()}}
	result := CalculateBalance(rows, map[string]types.RawAmount{pendingToken.String(): types.NewRawAmountFromInt64(5)}, map[id.ID]er.Resolution{usd: identity}, cfg, time.Now())
	require.Equal(t, "partial", result.Valuation.Total.Coverage)
	require.Equal(t, "7.00", *result.Valuation.Total.Amount)
	require.Equal(t, 1, result.Valuation.Total.UnvaluedCount)
	require.Equal(t, "none", result.Valuation.Pending.Coverage)
	require.Nil(t, result.Valuation.Pending.Amount)
	require.Equal(t, "complete", result.Valuation.Available.Coverage)
	require.Equal(t, "7.00", *result.Valuation.Available.Amount)
	zero := CalculateBalance([]BalanceRow{{TokenID: id.New(), RawAmount: types.ZeroRawAmount()}}, nil, nil, cfg, time.Now())
	require.Equal(t, "complete", zero.Valuation.Total.Coverage)
	require.Equal(t, "0.00", *zero.Valuation.Total.Amount)
	require.Equal(t, "currency_not_linked", zero.ByToken[0].Valuation.Reason)
}
func TestTotalRoundsAfterAggregationAndPendingAppliedOnce(t *testing.T) {
	currency, usd, token := id.New(), id.New(), id.New()
	cfg := er.Configuration{BaseCurrency: er.CurrencyRef{ID: usd, DecimalPlaces: 2}}
	rate := er.Resolution{Status: er.StatusFresh, SelectedQuote: &er.Quote{Rate: decimal.RequireFromString("0.005"), Multiplier: 1}}
	rows := []BalanceRow{{TokenID: token, CurrencyID: &currency, RawAmount: types.NewRawAmountFromInt64(1)}, {TokenID: token, CurrencyID: &currency, RawAmount: types.NewRawAmountFromInt64(1)}}
	result := CalculateBalance(rows, map[string]types.RawAmount{token.String(): types.NewRawAmountFromInt64(1)}, map[id.ID]er.Resolution{currency: rate}, cfg, time.Now())
	require.Len(t, result.ByToken, 1)
	require.Equal(t, "3", result.ByToken[0].RawAmount)
	require.Equal(t, "2", result.ByToken[0].AvailableRaw)
	require.Equal(t, "1", result.ByToken[0].PendingRaw)
	require.Equal(t, "0.02", *result.Valuation.Total.Amount)
	rows[1].TokenID = id.New()
	result = CalculateBalance(rows, nil, map[id.ID]er.Resolution{currency: rate}, cfg, time.Now())
	require.Equal(t, "0.01", *result.ByToken[0].Valuation.Total)
	require.Equal(t, "0.01", *result.ByToken[1].Valuation.Total)
	require.Equal(t, "0.01", *result.Valuation.Total.Amount)
}
