// Package workerjob provides the JobRecorder — a thin decorator that wraps
// background task functions and persists execution metadata to sys_worker_jobs.
package workerjob

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// Fn is the signature of a recordable background task.
// It returns the number of items processed and any error.
type Fn func(ctx context.Context) (itemsProcessed int, err error)

// Recorder wraps background task functions and persists execution records.
// It is best-effort: repository errors are logged, never propagated.
type Recorder struct {
	repo Repository
	log  *logger.Logger
}

// NewRecorder creates a Recorder. If repo is nil, recording is a no-op.
func NewRecorder(repo Repository, log *logger.Logger) *Recorder {
	return &Recorder{repo: repo, log: log.WithComponent("job_recorder")}
}

// Record executes fn and always saves a run record to sys_worker_jobs.
//
// Use for infrequent tasks (hourly cleanup, crypto.expiration) where every
// execution — including skipped ones — is meaningful to an operator.
//
// Usage:
//
//	recorder.Record(ctx, "crypto.expiration", "crypto", func(ctx context.Context) (int, error) {
//	    n, err := invoiceRepo.ExpireOverdue(ctx)
//	    return int(n), err
//	})
func (r *Recorder) Record(ctx context.Context, jobName, category string, fn Fn) {
	r.record(ctx, jobName, category, fn, false)
}

// RecordIfWork executes fn but only writes to sys_worker_jobs when the task
// actually did something (items > 0) or encountered an error.
//
// Use for high-frequency tasks (outbox.relay every 500ms) to prevent log spam.
// Silent idle ticks are silently dropped — errors and real work are always recorded.
//
// Usage:
//
//	recorder.RecordIfWork(ctx, "outbox.relay", "outbox", func(ctx context.Context) (int, error) {
//	    return relay.ProcessBatch(ctx)
//	})
func (r *Recorder) RecordIfWork(ctx context.Context, jobName, category string, fn Fn) {
	r.record(ctx, jobName, category, fn, true)
}

// record is the shared implementation.
// skipIfIdle=true → omits DB writes entirely when items==0 and err==nil.
func (r *Recorder) record(ctx context.Context, jobName, category string, fn Fn, skipIfIdle bool) {
	if r.repo == nil {
		_, _ = fn(ctx)
		return
	}

	startedAt := time.Now()
	n, fnErr := fn(ctx)

	// Fast path: high-frequency task with no work done and no error — skip entirely.
	if skipIfIdle && fnErr == nil && n == 0 {
		return
	}

	now := time.Now()
	durationMs := int(now.Sub(startedAt).Milliseconds())

	var status Status
	var errMsg *string
	switch {
	case fnErr != nil:
		status = StatusError
		msg := fnErr.Error()
		errMsg = &msg
	case n == 0:
		status = StatusSkipped
	default:
		status = StatusSuccess
	}

	job := &Job{
		ID:             id.New(),
		JobName:        jobName,
		JobCategory:    category,
		Status:         status,
		StartedAt:      startedAt,
		FinishedAt:     &now,
		DurationMs:     &durationMs,
		ItemsProcessed: &n,
		ErrorMessage:   errMsg,
	}

	// Single INSERT (no UPDATE needed — we write the final state directly).
	if err := r.repo.Insert(ctx, job); err != nil {
		r.log.Warnw("failed to insert worker job record", "job", jobName, "error", err)
	}
}
