package crypto_worker

import (
	"context"
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

const invoiceReconciliationPeriod = time.Minute
const merchantWebhookRecoveryPeriod = 5 * time.Second

type invoiceReconciliationCandidate = crypto_repo.WorkerInvoiceReconciliationCandidate

func (p *CryptoProcessor) runInvoiceReconciliationLoop(ctx context.Context) {
	ticker := time.NewTicker(invoiceReconciliationPeriod)
	defer ticker.Stop()
	var lastDaily time.Time
	for {
		daily := lastDaily.IsZero() || time.Since(lastDaily) >= 24*time.Hour
		if err := p.reconcileInvoiceProjections(ctx, daily); err != nil {
			p.log.Errorw("invoice reconciliation failed", "error", err)
		}
		if daily {
			lastDaily = time.Now().UTC()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *CryptoProcessor) reconcileInvoiceProjections(ctx context.Context, daily bool) error {
	if daily {
		if err := p.cleanupMerchantWebhookRetention(ctx); err != nil {
			return fmt.Errorf("clean merchant webhook retention: %w", err)
		}
	}
	candidates, err := p.loadInvoiceReconciliationCandidates(ctx, daily)
	if err != nil {
		return err
	}
	checked, repairs, failures := 0, 0, 0
	for _, candidate := range candidates {
		checked++
		_, reconcileErr := p.eventProcessor.ReconcileInvoice(ctx, candidate.ID)
		if reconcileErr != nil {
			failures++
			if candidate.Drift {
				_ = p.recordInvoiceDrift(ctx, candidate.ID, reconcileErr.Error())
			}
			continue
		}
		if candidate.Drift {
			stillDrifting, driftErr := p.invoiceProjectionHasDrift(ctx, candidate.ID)
			if driftErr != nil {
				failures++
				_ = p.recordInvoiceDrift(ctx, candidate.ID, driftErr.Error())
				continue
			}
			if stillDrifting {
				failures++
				_ = p.recordInvoiceDrift(ctx, candidate.ID, "projection still differs after lifecycle reconciliation")
				continue
			}
			repairs++
		}
		_ = p.resolveInvoiceDrift(ctx, candidate.ID)
	}
	recordInvoiceReconciliation(ctx, checked, repairs, failures, daily)
	return nil
}

func (p *CryptoProcessor) invoiceProjectionHasDrift(ctx context.Context, invoiceID id.ID) (bool, error) {
	return crypto_repo.NewWorkerRuntimeRepo().InvoiceProjectionHasDrift(ctx, invoiceID)
}

func (p *CryptoProcessor) loadInvoiceReconciliationCandidates(ctx context.Context, daily bool) ([]invoiceReconciliationCandidate, error) {
	return crypto_repo.NewWorkerRuntimeRepo().LoadInvoiceReconciliationCandidates(ctx, daily)
}

func (p *CryptoProcessor) recordInvoiceDrift(ctx context.Context, invoiceID id.ID, reason string) error {
	return crypto_repo.NewWorkerRuntimeRepo().RecordInvoiceDrift(ctx, invoiceID, reason)
}

func (p *CryptoProcessor) resolveInvoiceDrift(ctx context.Context, invoiceID id.ID) error {
	return crypto_repo.NewWorkerRuntimeRepo().ResolveInvoiceDrift(ctx, invoiceID)
}

func RecoverMerchantWebhookWork(ctx context.Context) error {
	repo := crypto_repo.NewWorkerRuntimeRepo()
	queueAge, err := repo.RecoverMerchantWebhookClaims(ctx)
	if err != nil {
		return err
	}
	recordWebhookQueueAge(ctx, queueAge)
	return repo.EnqueueMissingMerchantWebhooks(ctx)
}

func (p *CryptoProcessor) cleanupMerchantWebhookRetention(ctx context.Context) error {
	return crypto_repo.NewWorkerRuntimeRepo().CleanupMerchantWebhookRetention(ctx)
}
