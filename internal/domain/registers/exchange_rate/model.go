// Package exchange_rate owns immutable price observations and valuation rules.
package exchange_rate

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

const (
	FetchInterval      = 5 * time.Minute
	MaxAge             = 15 * time.Minute
	LeaseDuration      = 120 * time.Second
	LeaseRenewInterval = 30 * time.Second
)

type CurrencyRef struct {
	ID            id.ID  `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	DecimalPlaces int    `json:"decimalPlaces"`
}

type SourceRef struct {
	ID         id.ID  `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	SourceType string `json:"sourceType"`
}

type Quote struct {
	ID                id.ID           `json:"id"`
	Currency          CurrencyRef     `json:"currency"`
	BaseCurrency      CurrencyRef     `json:"baseCurrency"`
	Source            SourceRef       `json:"source"`
	Rate              decimal.Decimal `json:"rate"`
	Multiplier        int             `json:"multiplier"`
	ProviderUpdatedAt time.Time       `json:"providerUpdatedAt"`
	ReceivedAt        time.Time       `json:"receivedAt"`
	RecordedAt        time.Time       `json:"recordedAt"`
	Version           int             `json:"version"`
	MappingVersion    int             `json:"mappingVersion"`
	FeedAttemptID     id.ID           `json:"feedAttemptId"`
}

// ValidatePrice rejects values PostgreSQL NUMERIC(38,18) would round or overflow.
func ValidatePrice(rate decimal.Decimal, multiplier int) error {
	if !rate.IsPositive() || multiplier < 1 || int64(multiplier) > 2147483647 {
		return apperror.NewValidation("Курс и кратность должны быть больше нуля")
	}
	digits := rate.Coefficient().String()
	trailing := len(digits) - len(strings.TrimRight(digits, "0"))
	if int64(len(digits))+int64(rate.Exponent()) > 20 || int64(rate.Exponent())+int64(trailing) < -18 {
		return apperror.NewBusinessRule("RATE_PRECISION_EXCEEDED", "Допустимо до 20 цифр до запятой и 18 после неё")
	}
	return nil
}

// ConvertExact never rounds before a valuation bucket has been aggregated.
func ConvertExact(amount, rate decimal.Decimal, multiplier int) *big.Rat {
	return new(big.Rat).Quo(new(big.Rat).Mul(amount.Rat(), rate.Rat()), big.NewRat(int64(multiplier), 1))
}

// RoundAmount uses half away from zero, including for negative values.
func RoundAmount(value *big.Rat, places int) string {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
	n := new(big.Int).Mul(value.Num(), scale)
	sign := n.Sign()
	n.Abs(n)
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, value.Denom(), r)
	if r.Lsh(r, 1).Cmp(value.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if sign < 0 {
		q.Neg(q)
	}
	return decimal.NewFromBigInt(q, -int32(places)).StringFixed(int32(places))
}

type Mapping struct {
	Currency     CurrencyRef `json:"currency"`
	BaseCurrency CurrencyRef `json:"baseCurrency"`
	SourceID     id.ID       `json:"sourceId"`
	ExternalID   string      `json:"externalId"`
	IsActive     bool        `json:"isActive"`
	Version      int         `json:"version"`
}

type Configuration struct {
	IntegrationPoolID *id.ID      `json:"-"`
	BaseCurrency      CurrencyRef `json:"baseCurrency"`
	Source            *SourceRef  `json:"source"`
	SourceActive      bool        `json:"sourceActive"`
	SourceVersion     int         `json:"-"`
	SettingsVersion   int         `json:"-"`
	RateLimitRPM      int         `json:"-"`
	APIKey            string      `json:"-"`
}

type Candidate struct {
	Currency CurrencyRef
	Mapping  *Mapping
	Quote    *Quote
}

type Status string

const (
	StatusFresh    Status = "fresh"
	StatusStale    Status = "stale"
	StatusMissing  Status = "missing"
	StatusDisabled Status = "disabled"
	StatusIdentity Status = "identity"
)

type Resolution struct {
	Currency       *CurrencyRef `json:"currency"`
	BaseCurrency   CurrencyRef  `json:"baseCurrency"`
	Status         Status       `json:"status"`
	Reason         string       `json:"reason"`
	SelectedQuote  *Quote       `json:"selectedQuote"`
	LastKnownQuote *Quote       `json:"lastKnownQuote"`
	EvaluatedAt    time.Time    `json:"evaluatedAt"`
	FreshUntil     *time.Time   `json:"freshUntil"`
}

func Resolve(candidate Candidate, cfg Configuration, at time.Time) Resolution {
	currency := candidate.Currency
	r := Resolution{Currency: &currency, BaseCurrency: cfg.BaseCurrency, Status: StatusMissing,
		Reason: "no_quote", LastKnownQuote: candidate.Quote, EvaluatedAt: at}
	if currency.ID == cfg.BaseCurrency.ID {
		r.Status, r.Reason = StatusIdentity, "same_currency"
		return r
	}
	if cfg.Source == nil {
		r.Reason = "source_not_configured"
		return r
	}
	if !cfg.SourceActive {
		r.Status, r.Reason = StatusDisabled, "source_disabled"
		return r
	}
	if candidate.Mapping == nil {
		r.Reason = "mapping_missing"
		return r
	}
	if !candidate.Mapping.IsActive {
		r.Status, r.Reason = StatusDisabled, "mapping_disabled"
		return r
	}
	q := candidate.Quote
	if q == nil {
		return r
	}
	if q.MappingVersion != candidate.Mapping.Version {
		r.Reason = "mapping_changed"
		return r
	}
	if q.ProviderUpdatedAt.After(at) {
		r.Reason = "quote_not_effective"
		return r
	}
	until := q.ProviderUpdatedAt.Add(MaxAge)
	r.FreshUntil = &until
	if !at.Before(until) {
		r.Status, r.Reason = StatusStale, "quote_stale"
		return r
	}
	r.Status, r.Reason, r.SelectedQuote = StatusFresh, "selected", q
	return r
}

type Snapshot func(context.Context, func(context.Context) error) error

type Repository interface {
	LoadConfiguration(context.Context) (Configuration, error)
	Candidates(context.Context, Configuration, []id.ID, time.Time) ([]Candidate, error)
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }
func (s *Service) Configuration(ctx context.Context) (Configuration, error) {
	return s.repo.LoadConfiguration(ctx)
}
func (s *Service) ResolveBatch(ctx context.Context, cfg Configuration, currencies []id.ID, at time.Time) (map[id.ID]Resolution, error) {
	unique := make([]id.ID, 0, len(currencies))
	seen := make(map[id.ID]bool, len(currencies))
	for _, c := range currencies {
		if !id.IsNil(c) && !seen[c] {
			seen[c] = true
			unique = append(unique, c)
		}
	}
	out := make(map[id.ID]Resolution, len(unique))
	if len(unique) == 0 {
		return out, nil
	}
	candidates, err := s.repo.Candidates(ctx, cfg, unique, at)
	if err != nil {
		return nil, err
	}
	for _, c := range candidates {
		out[c.Currency.ID] = Resolve(c, cfg, at)
	}
	return out, nil
}
