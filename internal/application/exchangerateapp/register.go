package exchangerateapp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	er "github.com/alex-bogatiuk/metapus/internal/domain/registers/exchange_rate"
)

type RegisterService struct {
	repo     er.ReadRepository
	snapshot er.Snapshot
}

func NewRegisterService(repo er.ReadRepository, snapshot er.Snapshot) *RegisterService {
	return &RegisterService{repo, snapshot}
}

type CurrentRequest struct {
	CurrencyID *id.ID
	Status     er.Status
	Cursor     string
	Limit      int
}
type FeedHealth struct {
	Status        string     `json:"status"`
	LastAttemptAt *time.Time `json:"lastAttemptAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
}
type CurrentPage struct {
	FeedHealth *FeedHealth `json:"feedHealth"`
	er.Configuration
	EvaluatedAt     time.Time       `json:"evaluatedAt"`
	IntervalSeconds int             `json:"intervalSeconds"`
	MaxAgeSeconds   int             `json:"maxAgeSeconds"`
	Items           []er.Resolution `json:"items"`
	NextCursor      *string         `json:"nextCursor"`
}
type HistoryRequest struct {
	CurrencyID     id.ID
	BaseCurrencyID *id.ID
	SourceID       *id.ID
	From, To       *time.Time
	Cursor         string
	Limit          int
}
type HistoryPage struct {
	Items      []er.Quote `json:"items"`
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	NextCursor *string    `json:"nextCursor"`
}
type pageCursor struct {
	Scope    string
	At       time.Time
	From, To time.Time
	Code     string
	ID       id.ID
	Version  int
}

func scope(value any) string {
	raw, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func decodeCursor(token string, expected string) (pageCursor, error) {
	var c pageCursor
	if token == "" {
		return c, nil
	}
	if len(token) > 2048 {
		return c, apperror.NewValidation("Некорректный курсор")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || json.Unmarshal(raw, &c) != nil || c.Scope != expected || id.IsNil(c.ID) {
		return c, apperror.NewValidation("Курсор не соответствует фильтрам")
	}
	return c, nil
}
func encodeCursor(c pageCursor) *string {
	raw, _ := json.Marshal(c)
	token := base64.RawURLEncoding.EncodeToString(raw)
	return &token
}
func pageSize(n int) (int, error) {
	if n == 0 {
		return 50, nil
	}
	if n < 1 || n > 100 {
		return 0, apperror.NewValidation("Размер страницы должен быть от 1 до 100")
	}
	return n, nil
}
func (s *RegisterService) Current(ctx context.Context, req CurrentRequest) (CurrentPage, error) {
	out := CurrentPage{Items: []er.Resolution{}, IntervalSeconds: int(er.FetchInterval.Seconds()), MaxAgeSeconds: int(er.MaxAge.Seconds())}
	limit, err := pageSize(req.Limit)
	if err != nil {
		return out, err
	}
	switch req.Status {
	case "", er.StatusFresh, er.StatusStale, er.StatusMissing, er.StatusDisabled, er.StatusIdentity:
	default:
		return out, apperror.NewValidation("Неизвестное состояние курса")
	}
	key := scope(struct {
		Kind     string
		Currency *id.ID
		Status   er.Status
	}{"current", req.CurrencyID, req.Status})
	cursor, err := decodeCursor(req.Cursor, key)
	if err != nil {
		return out, err
	}
	out.EvaluatedAt = time.Now().UTC()
	err = s.snapshot(ctx, func(ctx context.Context) error {
		cfg, e := s.repo.LoadConfiguration(ctx)
		if e != nil {
			return e
		}
		out.Configuration = cfg
		if cfg.Source != nil && cfg.SourceActive {
			feed, e := s.repo.FeedState(ctx, cfg.Source.ID, cfg.BaseCurrency.ID)
			if e != nil {
				return e
			}
			out.FeedHealth = &FeedHealth{Status: feed.Status, LastAttemptAt: feed.LastAttemptAt, LastSuccessAt: feed.LastSuccessAt}
		}
		rows, e := s.repo.Current(ctx, cfg, er.CurrentFilter{CurrencyID: req.CurrencyID, Status: req.Status, AfterCode: cursor.Code, AfterID: cursor.ID, Limit: limit + 1}, out.EvaluatedAt)
		if e != nil {
			return e
		}
		if len(rows) > limit {
			rows = rows[:limit]
			last := rows[len(rows)-1]
			out.NextCursor = encodeCursor(pageCursor{Scope: key, Code: last.Currency.Code, ID: last.Currency.ID})
		}
		for _, row := range rows {
			out.Items = append(out.Items, er.Resolve(row, cfg, out.EvaluatedAt))
		}
		return nil
	})
	return out, err
}
func (s *RegisterService) History(ctx context.Context, req HistoryRequest) (HistoryPage, error) {
	out := HistoryPage{Items: []er.Quote{}}
	limit, err := pageSize(req.Limit)
	if err != nil {
		return out, err
	}
	if id.IsNil(req.CurrencyID) {
		return out, apperror.NewValidation("Укажите валюту")
	}
	key := scope(struct {
		Kind         string
		Currency     id.ID
		Base, Source *id.ID
		From, To     *time.Time
	}{"history", req.CurrencyID, req.BaseCurrencyID, req.SourceID, req.From, req.To})
	cursor, err := decodeCursor(req.Cursor, key)
	if err != nil {
		return out, err
	}
	out.To = time.Now().UTC()
	if req.To != nil {
		out.To = *req.To
	}
	out.From = out.To.Add(-24 * time.Hour)
	if req.From != nil {
		out.From = *req.From
	}
	if req.Cursor != "" {
		out.From, out.To = cursor.From, cursor.To
	}
	if !out.From.Before(out.To) || out.To.Sub(out.From) > 31*24*time.Hour {
		return out, apperror.NewValidation("Период истории должен быть больше нуля и не превышать 31 день")
	}
	err = s.snapshot(ctx, func(ctx context.Context) error {
		cfg, e := s.repo.LoadConfiguration(ctx)
		if e != nil {
			return e
		}
		base := cfg.BaseCurrency.ID
		if req.BaseCurrencyID != nil {
			base = *req.BaseCurrencyID
		}
		f := er.HistoryFilter{CurrencyID: req.CurrencyID, BaseCurrencyID: base, SourceID: req.SourceID, From: out.From, To: out.To, Limit: limit + 1, AfterID: cursor.ID, AfterVersion: cursor.Version}
		if req.Cursor != "" {
			f.AfterTime = &cursor.At
		}
		rows, e := s.repo.History(ctx, f)
		if e != nil {
			return e
		}
		if len(rows) > limit {
			rows = rows[:limit]
			last := rows[len(rows)-1]
			out.NextCursor = encodeCursor(pageCursor{Scope: key, From: out.From, To: out.To, At: last.ProviderUpdatedAt, Version: last.Version, ID: last.ID})
		}
		out.Items = rows
		return nil
	})
	return out, err
}
func (s *RegisterService) Quote(ctx context.Context, quoteID id.ID) (*er.Quote, error) {
	return s.repo.GetQuote(ctx, quoteID)
}

type MappingPage struct {
	Items        []er.Mapping   `json:"items"`
	BaseCurrency er.CurrencyRef `json:"baseCurrency"`
}

func (s *RegisterService) Mappings(ctx context.Context, sourceID id.ID) (MappingPage, error) {
	var out MappingPage
	err := s.snapshot(ctx, func(ctx context.Context) error {
		cfg, err := s.repo.LoadConfiguration(ctx)
		if err != nil {
			return err
		}
		out.BaseCurrency = cfg.BaseCurrency
		out.Items, err = s.repo.Mappings(ctx, sourceID, cfg.BaseCurrency.ID)
		return err
	})
	return out, err
}
func (s *RegisterService) PutMapping(ctx context.Context, m er.Mapping, expected int) (er.Mapping, error) {
	return s.repo.PutMapping(ctx, m, expected)
}
func (s *RegisterService) FeedState(ctx context.Context, sourceID id.ID) (er.FeedState, error) {
	cfg, err := s.repo.LoadConfiguration(ctx)
	if err != nil {
		return er.FeedState{}, err
	}
	return s.repo.FeedState(ctx, sourceID, cfg.BaseCurrency.ID)
}
