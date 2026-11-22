// Package exchange_rate is the platform's exchange rate register: rate
// resolution and conversion of amounts.
package exchange_rate

import (
	"math/big"

	"github.com/shopspring/decimal"

	core "metapus/internal/domain/registers/exchange_rate"
)

type (
	Candidate     = core.Candidate
	Configuration = core.Configuration
	CurrencyRef   = core.CurrencyRef
	Mapping       = core.Mapping
	Quote         = core.Quote
	Resolution    = core.Resolution
	Service       = core.Service
	Snapshot      = core.Snapshot
	SourceRef     = core.SourceRef
	Status        = core.Status
)

const (
	StatusFresh    = core.StatusFresh
	StatusIdentity = core.StatusIdentity
	StatusMissing  = core.StatusMissing
)

// ConvertExact never rounds before a valuation bucket has been aggregated.
func ConvertExact(amount decimal.Decimal, rate decimal.Decimal, multiplier int) *big.Rat {
	return core.ConvertExact(amount, rate, multiplier)
}

func NewService(repo core.Repository) *core.Service { return core.NewService(repo) }

// RoundAmount uses half away from zero, including for negative values.
func RoundAmount(value *big.Rat, places int) string { return core.RoundAmount(value, places) }
