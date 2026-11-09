package exchange_rate

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/id"
)

type Problem struct {
	CurrencyID *id.ID `json:"currencyId"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}
type FeedState struct {
	SourceID       id.ID      `json:"sourceId"`
	BaseCurrencyID id.ID      `json:"baseCurrencyId"`
	Status         string     `json:"status"`
	NextRunAt      *time.Time `json:"nextRunAt"`
	LastAttemptAt  *time.Time `json:"lastAttemptAt"`
	LastSuccessAt  *time.Time `json:"lastSuccessAt"`
	Requested      int        `json:"requested"`
	Received       int        `json:"received"`
	Inserted       int        `json:"inserted"`
	Repeated       int        `json:"repeated"`
	Rejected       int        `json:"rejected"`
	Problems       []Problem  `json:"problems"`
}
type Observation struct {
	Mapping           Mapping
	Rate              decimal.Decimal
	ProviderUpdatedAt time.Time
	ReceivedAt        time.Time
}
type FetchResult struct {
	Observations []Observation
	Problems     []Problem
	Requested    int
	RetryAt      *time.Time
}
type FeedRepository interface {
	Repository
	Mappings(context.Context, id.ID, id.ID) ([]Mapping, error)
	Claim(context.Context, Configuration, id.ID) (bool, error)
	Renew(context.Context, Configuration, id.ID) (bool, error)
	Finish(context.Context, Configuration, id.ID, FetchResult) (FeedState, error)
	Fail(context.Context, Configuration, id.ID, string, int) error
}

type CurrentFilter struct {
	CurrencyID *id.ID
	Status     Status
	AfterCode  string
	AfterID    id.ID
	Limit      int
}
type HistoryFilter struct {
	CurrencyID     id.ID
	BaseCurrencyID id.ID
	SourceID       *id.ID
	From           time.Time
	To             time.Time
	AfterTime      *time.Time
	AfterVersion   int
	AfterID        id.ID
	Limit          int
}
type ReadRepository interface {
	Repository
	Current(context.Context, Configuration, CurrentFilter, time.Time) ([]Candidate, error)
	History(context.Context, HistoryFilter) ([]Quote, error)
	GetQuote(context.Context, id.ID) (*Quote, error)
	Mappings(context.Context, id.ID, id.ID) ([]Mapping, error)
	PutMapping(context.Context, Mapping, int) (Mapping, error)
	FeedState(context.Context, id.ID, id.ID) (FeedState, error)
}
