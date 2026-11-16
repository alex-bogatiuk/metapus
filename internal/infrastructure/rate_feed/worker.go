package rate_feed

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain/integrations"
	er "metapus/internal/domain/registers/exchange_rate"
	integrationhttp "metapus/internal/infrastructure/integrations"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
	"metapus/pkg/logger"
	"net/http"
)

// feedCatalog holds the platform providers; the feed reads only CoinGecko pools.
var feedCatalog = integrations.PlatformCatalog()

type Worker struct {
	repo     er.FeedRepository
	fetcher  *CoinGeckoFetcher
	recorder *workerjob.Recorder
	log      *logger.Logger
}

func NewWorker(repo er.FeedRepository, fetcher *CoinGeckoFetcher, recorder *workerjob.Recorder, log *logger.Logger) *Worker {
	return &Worker{repo: repo, fetcher: fetcher, recorder: recorder, log: log.WithComponent("rate_feed")}
}
func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if w.recorder != nil {
			w.recorder.RecordIfWork(ctx, "rate_feed.coingecko", "rate_feed", w.doFetch)
		} else {
			if _, err := w.doFetch(ctx); err != nil {
				w.log.Errorw("rate feed failed", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (w *Worker) doFetch(ctx context.Context) (int, error) {
	cfg, err := w.repo.LoadConfiguration(ctx)
	if err != nil {
		return 0, err
	}
	if cfg.Source == nil || !cfg.SourceActive {
		return 0, nil
	}
	token := id.New()
	claimed, err := w.repo.Claim(ctx, cfg, token)
	if err != nil || !claimed {
		return 0, err
	}
	mappings, err := w.repo.Mappings(ctx, cfg.Source.ID, cfg.BaseCurrency.ID)
	if err != nil {
		if failErr := w.repo.Fail(ctx, cfg, token, "Не удалось прочитать соответствия", 0); failErr != nil {
			return 0, fmt.Errorf("read mappings: %w; record failure: %v", err, failErr)
		}
		return 0, err
	}
	active := make([]er.Mapping, 0, len(mappings))
	for _, m := range mappings {
		if m.IsActive {
			active = append(active, m)
		}
	}
	workCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(er.LeaseRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				ok, e := w.repo.Renew(workCtx, cfg, token)
				if e != nil || !ok {
					cancel()
					return
				}
			}
		}
	}()
	fetcher := w.fetcher
	apiKey := cfg.APIKey
	if cfg.IntegrationPoolID != nil {
		copyFetcher := *w.fetcher
		client := *w.fetcher.client
		client.Transport = &integrationhttp.Transport{Base: client.Transport, Repository: integration_repo.NewRuntime(feedCatalog), PoolID: *cfg.IntegrationPoolID, Provider: integrations.CoinGeckoDemo}
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		copyFetcher.client = &client
		fetcher = &copyFetcher
		apiKey = "managed-by-integration-pool"
	}
	result := fetcher.FetchRates(workCtx, active, cfg.RateLimitRPM, apiKey)
	// Join the renewal goroutine before committing; no unowned background work.
	cancel()
	<-done
	state, err := w.repo.Finish(ctx, cfg, token, result)
	if err != nil {
		if failErr := w.repo.Fail(ctx, cfg, token, "Не удалось сохранить котировки", result.Requested); failErr != nil {
			return 0, fmt.Errorf("save rates: %w; record failure: %v", err, failErr)
		}
		return 0, err
	}
	if state.Status == "failed" || state.Status == "partial" {
		return state.Received, fmt.Errorf("получены курсы для %d из %d пар: %s", state.Received, state.Requested, state.Status)
	}
	return max(1, state.Received), nil
}
