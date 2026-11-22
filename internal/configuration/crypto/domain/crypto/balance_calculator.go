package crypto

import (
	"context"
	"math/big"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/registers/exchange_rate"
	"metapus/pkg/sdk/types"
)

type BalanceRow struct {
	MerchantID    id.ID           `db:"merchant_id"`
	TokenID       id.ID           `db:"token_id"`
	TokenSymbol   string          `db:"symbol"`
	NetworkID     id.ID           `db:"network_id"`
	Network       string          `db:"network"`
	DecimalPlaces int             `db:"decimal_places"`
	CurrencyID    *id.ID          `db:"currency_id"`
	CurrencyCode  string          `db:"iso_code"`
	RawAmount     types.RawAmount `db:"amount"`
}
type BalanceQueryRepo interface {
	GetMerchantBalances(context.Context, []id.ID) ([]BalanceRow, error)
	GetPendingAmounts(context.Context, []id.ID) (map[string]types.RawAmount, error)
}

// QuoteDetails intentionally omits ingestion and mapping configuration from Portal.
type QuoteDetails struct {
	ID                id.ID                     `json:"id"`
	Currency          exchange_rate.CurrencyRef `json:"currency"`
	BaseCurrency      exchange_rate.CurrencyRef `json:"baseCurrency"`
	Source            exchange_rate.SourceRef   `json:"source"`
	Rate              decimal.Decimal           `json:"rate"`
	Multiplier        int                       `json:"multiplier"`
	Version           int                       `json:"version"`
	ProviderUpdatedAt time.Time                 `json:"providerUpdatedAt"`
	ReceivedAt        time.Time                 `json:"receivedAt"`
	RecordedAt        time.Time                 `json:"recordedAt"`
}

func quoteDetails(q *exchange_rate.Quote) *QuoteDetails {
	if q == nil {
		return nil
	}
	return &QuoteDetails{ID: q.ID, Currency: q.Currency, BaseCurrency: q.BaseCurrency, Source: q.Source, Rate: q.Rate, Multiplier: q.Multiplier, Version: q.Version, ProviderUpdatedAt: q.ProviderUpdatedAt, ReceivedAt: q.ReceivedAt, RecordedAt: q.RecordedAt}
}

type TokenValuation struct {
	Status         exchange_rate.Status `json:"status"`
	Reason         string               `json:"reason"`
	SelectedQuote  *QuoteDetails        `json:"selectedQuote"`
	LastKnownQuote *QuoteDetails        `json:"lastKnownQuote"`
	FreshUntil     *time.Time           `json:"freshUntil"`
	Total          *string              `json:"total"`
	Pending        *string              `json:"pending"`
	Available      *string              `json:"available"`
}
type TokenBalance struct {
	TokenID        id.ID           `json:"tokenId"`
	TokenSymbol    string          `json:"tokenSymbol"`
	NetworkID      string          `json:"networkId,omitempty"`
	Network        string          `json:"network,omitempty"`
	DecimalPlaces  int             `json:"decimalPlaces"`
	CurrencyCode   string          `json:"currencyCode"`
	RawAmount      string          `json:"rawAmount"`
	HumanAmount    decimal.Decimal `json:"humanAmount"`
	PendingRaw     string          `json:"pendingRaw"`
	PendingHuman   decimal.Decimal `json:"pendingHuman"`
	AvailableRaw   string          `json:"availableRaw"`
	AvailableHuman decimal.Decimal `json:"availableHuman"`
	Valuation      TokenValuation  `json:"valuation"`
}
type ValuationBucket struct {
	Amount        *string `json:"amount"`
	Coverage      string  `json:"coverage"`
	UnvaluedCount int     `json:"unvaluedCount"`
}
type BalanceValuation struct {
	Total     ValuationBucket `json:"total"`
	Pending   ValuationBucket `json:"pending"`
	Available ValuationBucket `json:"available"`
}
type MerchantBalance struct {
	BaseCurrency exchange_rate.CurrencyRef `json:"baseCurrency"`
	Source       *exchange_rate.SourceRef  `json:"source"`
	EvaluatedAt  time.Time                 `json:"evaluatedAt"`
	FreshUntil   *time.Time                `json:"freshUntil"`
	Valuation    BalanceValuation          `json:"valuation"`
	ByToken      []TokenBalance            `json:"byToken"`
}
type bucketAccumulator struct {
	sum     *big.Rat
	valued  int
	missing int
}

func newBucket() *bucketAccumulator { return &bucketAccumulator{sum: new(big.Rat)} }
func (b *bucketAccumulator) add(amount decimal.Decimal, res exchange_rate.Resolution, places int) *string {
	if amount.IsZero() {
		zero := decimal.Zero.StringFixed(int32(places))
		return &zero
	}
	var exact *big.Rat
	switch res.Status {
	case exchange_rate.StatusIdentity:
		exact = amount.Rat()
	case exchange_rate.StatusFresh:
		if res.SelectedQuote != nil {
			exact = exchange_rate.ConvertExact(amount, res.SelectedQuote.Rate, res.SelectedQuote.Multiplier)
		}
	}
	if exact == nil {
		b.missing++
		return nil
	}
	b.valued++
	b.sum.Add(b.sum, exact)
	s := exchange_rate.RoundAmount(exact, places)
	return &s
}
func (b *bucketAccumulator) result(places int) ValuationBucket {
	r := ValuationBucket{Coverage: "complete", UnvaluedCount: b.missing}
	if b.missing > 0 {
		r.Coverage = "partial"
		if b.valued == 0 {
			r.Coverage = "none"
			return r
		}
	}
	s := exchange_rate.RoundAmount(b.sum, places)
	r.Amount = &s
	return r
}

// CalculateBalance is pure: one immutable set of quantities and resolutions.
func CalculateBalance(rows []BalanceRow, pending map[string]types.RawAmount, rates map[id.ID]exchange_rate.Resolution, cfg exchange_rate.Configuration, at time.Time) MerchantBalance {
	result := MerchantBalance{BaseCurrency: cfg.BaseCurrency, Source: cfg.Source, EvaluatedAt: at, ByToken: make([]TokenBalance, 0, len(rows))}
	// Scope may include multiple merchants; pending totals must be applied once per token.
	byToken := make(map[id.ID]BalanceRow, len(rows))
	for _, row := range rows {
		if old, ok := byToken[row.TokenID]; ok {
			old.RawAmount = old.RawAmount.Add(row.RawAmount)
			byToken[row.TokenID] = old
		} else {
			byToken[row.TokenID] = row
		}
	}
	ids := make([]id.ID, 0, len(byToken))
	for token := range byToken {
		ids = append(ids, token)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := byToken[ids[i]], byToken[ids[j]]
		if a.TokenSymbol == b.TokenSymbol {
			return ids[i].String() < ids[j].String()
		}
		return a.TokenSymbol < b.TokenSymbol
	})
	totalBucket, pendingBucket, availableBucket := newBucket(), newBucket(), newBucket()
	for _, tokenID := range ids {
		row := byToken[tokenID]
		pendingRaw := pending[tokenID.String()]
		// The register holds confirmed net funds; pending receipts are additional funds.
		availableRaw := row.RawAmount
		totalRaw := availableRaw.Add(pendingRaw)
		tb := TokenBalance{TokenID: tokenID, TokenSymbol: row.TokenSymbol, Network: row.Network, DecimalPlaces: row.DecimalPlaces, CurrencyCode: row.CurrencyCode,
			RawAmount: totalRaw.String(), HumanAmount: totalRaw.ToDecimal(row.DecimalPlaces), PendingRaw: pendingRaw.String(), PendingHuman: pendingRaw.ToDecimal(row.DecimalPlaces), AvailableRaw: availableRaw.String(), AvailableHuman: availableRaw.ToDecimal(row.DecimalPlaces)}
		if !id.IsNil(row.NetworkID) {
			tb.NetworkID = row.NetworkID.String()
		}
		res := exchange_rate.Resolution{Status: exchange_rate.StatusMissing, Reason: "currency_not_linked", BaseCurrency: cfg.BaseCurrency, EvaluatedAt: at}
		if row.CurrencyID != nil {
			if selected, ok := rates[*row.CurrencyID]; ok {
				res = selected
			} else {
				res.Reason = "currency_missing"
			}
		}
		tb.Valuation = TokenValuation{Status: res.Status, Reason: res.Reason, SelectedQuote: quoteDetails(res.SelectedQuote), LastKnownQuote: quoteDetails(res.LastKnownQuote), FreshUntil: res.FreshUntil}
		tb.Valuation.Total = totalBucket.add(tb.HumanAmount, res, cfg.BaseCurrency.DecimalPlaces)
		tb.Valuation.Pending = pendingBucket.add(tb.PendingHuman, res, cfg.BaseCurrency.DecimalPlaces)
		tb.Valuation.Available = availableBucket.add(tb.AvailableHuman, res, cfg.BaseCurrency.DecimalPlaces)
		if res.Status == exchange_rate.StatusFresh && res.FreshUntil != nil && (result.FreshUntil == nil || res.FreshUntil.Before(*result.FreshUntil)) {
			until := *res.FreshUntil
			result.FreshUntil = &until
		}
		result.ByToken = append(result.ByToken, tb)
	}
	result.Valuation = BalanceValuation{Total: totalBucket.result(cfg.BaseCurrency.DecimalPlaces), Pending: pendingBucket.result(cfg.BaseCurrency.DecimalPlaces), Available: availableBucket.result(cfg.BaseCurrency.DecimalPlaces)}
	return result
}
