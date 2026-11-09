package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"metapus/internal/core/workerjob"
	"metapus/internal/infrastructure/storage/postgres"
)

func (w *MultiTenantWorker) setReady(ctx context.Context, tenantID, component string, ready bool) {
	value := int64(0)
	if ready {
		value = 1
	}
	w.ready.Record(ctx, value, metric.WithAttributes(attribute.String("tenant_id", tenantID), attribute.String("component", component)))
}

func (w *MultiTenantWorker) supervise(ctx context.Context, tenantID, component string, run func(context.Context) error) {
	w.setReady(ctx, tenantID, component, false)
	workerjob.Supervise(ctx, time.Second, time.Minute, run, func(err error, delay time.Duration) {
		w.setReady(ctx, tenantID, component, false)
		w.restarts.Add(ctx, 1, metric.WithAttributes(attribute.String("tenant_id", tenantID), attribute.String("component", component)))
		w.log.Errorw("background component restarting", "tenant_id", tenantID, "component", component, "error", err, "retry_after", delay)
	})
	w.setReady(context.WithoutCancel(ctx), tenantID, component, false)
}

func (w *MultiTenantWorker) runOutboxLoop(ctx context.Context, recorder *workerjob.Recorder, name string, relay *postgres.OutboxRelay) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		processed := 0
		recorder.RecordIfWork(ctx, name, "outbox", func(ctx context.Context) (int, error) {
			var err error
			processed, err = relay.ProcessBatch(ctx)
			return processed, err
		})
		if processed > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func validateWorkerSchema(ctx context.Context, pool *pgxpool.Pool) error {
	var ready bool
	err := pool.QueryRow(ctx, `SELECT
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='sys_outbox' AND column_name IN ('claim_token','claimed_at','lease_until'))=3
  AND (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='sys_webhook_delivery_jobs' AND column_name IN ('claim_token','lease_until'))=2
  AND to_regclass('sys_automation_runs') IS NOT NULL AND to_regclass('sys_automation_schedules') IS NOT NULL
  AND to_regclass('uq_webhook_delivery_jobs_processing_merchant') IS NOT NULL
  AND to_regclass('uq_automation_runs_processing_rule') IS NOT NULL`).Scan(&ready)
	if err != nil {
		return fmt.Errorf("validate worker schema: %w", err)
	}
	if !ready {
		return fmt.Errorf("worker reliability schema is missing; apply db/upgrades/worker_reliability.sql with old workers stopped")
	}
	return nil
}
