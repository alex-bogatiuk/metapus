package crypto_worker

import (
	"context"
	"sync"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/workerjob"
)

// WebhookDeliveryWorker owns HTTP delivery independently of chain ingestion.
// Each executor claims one job, so no acquired lease waits in a local queue.
type WebhookDeliveryWorker struct {
	repo     crypto.MerchantWebhookRepository
	handler  *MerchantWebhookHandler
	recorder *workerjob.Recorder
}

func NewWebhookDeliveryWorker(repo crypto.MerchantWebhookRepository, handler *MerchantWebhookHandler, recorder *workerjob.Recorder) *WebhookDeliveryWorker {
	return &WebhookDeliveryWorker{repo: repo, handler: handler, recorder: recorder}
}

func (w *WebhookDeliveryWorker) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for ctx.Err() == nil {
				claimed := false
				w.recorder.RecordIfWork(ctx, "webhook.deliver", "outbox", func(ctx context.Context) (int, error) {
					jobs, err := w.repo.ClaimDueJobs(ctx, time.Now().UTC(), 1)
					if err != nil || len(jobs) == 0 {
						return 0, err
					}
					claimed = true
					return 1, w.handler.DeliverJob(ctx, jobs[0])
				})
				if claimed {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		})
	}
	ticker := time.NewTicker(merchantWebhookRecoveryPeriod)
	defer ticker.Stop()
	for {
		w.recorder.RecordIfWork(ctx, "webhook.recover", "outbox", func(ctx context.Context) (int, error) {
			return 0, RecoverMerchantWebhookWork(ctx)
		})
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
		}
	}
}
