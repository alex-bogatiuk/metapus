package portalapp

import (
	"context"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/exchange_rate"
	"github.com/shopspring/decimal"
	"strings"
	"time"
)

type WithdrawalTokenCurrencyReader interface {
	GetWithdrawalTokenCurrency(context.Context, id.ID) (*id.ID, error)
}
type WithdrawalValuationService struct {
	repo  WithdrawalTokenCurrencyReader
	rates *exchange_rate.Service
	now   func() time.Time
}

func NewWithdrawalValuationService(repo WithdrawalTokenCurrencyReader, rates *exchange_rate.Service) *WithdrawalValuationService {
	return &WithdrawalValuationService{repo: repo, rates: rates, now: time.Now}
}
func (h *WithdrawalValuationService) Evaluate(ctx context.Context, item AdminWithdrawalRequestDetail) *WithdrawalRequestUSDValuation {
	at := h.now().UTC()
	result := &WithdrawalRequestUSDValuation{Status: "unavailable", EvaluatedAt: at.Format(time.RFC3339)}
	if h.rates == nil {
		return result
	}
	cfg, err := h.rates.Configuration(ctx)
	if err != nil || !strings.EqualFold(cfg.BaseCurrency.Code, "USD") {
		return result
	}
	tokenID, err := id.Parse(item.TokenID)
	if err != nil {
		return result
	}
	currencyID, err := h.repo.GetWithdrawalTokenCurrency(ctx, tokenID)
	if err != nil || currencyID == nil {
		return result
	}
	resolved, err := h.rates.ResolveBatch(ctx, cfg, []id.ID{*currencyID}, at)
	if err != nil {
		return result
	}
	resolution, ok := resolved[*currencyID]
	if !ok {
		return result
	}
	result.Status = string(resolution.Status)
	raw, err := decimal.NewFromString(item.Amount)
	if err != nil || item.DecimalPlaces < 0 || item.DecimalPlaces > 18 {
		result.Status = "unavailable"
		return result
	}
	human := raw.Shift(-int32(item.DecimalPlaces))
	if resolution.Status == exchange_rate.StatusIdentity {
		amount := human.StringFixed(int32(cfg.BaseCurrency.DecimalPlaces))
		result.Amount = &amount
		return result
	}
	if resolution.Status != exchange_rate.StatusFresh || resolution.SelectedQuote == nil {
		return result
	}
	quote := resolution.SelectedQuote
	amount := exchange_rate.RoundAmount(exchange_rate.ConvertExact(human, quote.Rate, quote.Multiplier), cfg.BaseCurrency.DecimalPlaces)
	rate := quote.Rate.String()
	source := quote.Source.Name
	rateAsOf := quote.ProviderUpdatedAt.Format(time.RFC3339)
	result.Amount = &amount
	result.Rate = &rate
	result.Multiplier = quote.Multiplier
	result.Source = &source
	result.RateAsOf = &rateAsOf
	return result
}
