package cryptoapp

import (
	"context"
	"metapus/pkg/logger"
	"time"
)

type AMLMaintenanceRepository interface {
	ApplyNextAMLDecision(ctx context.Context, now time.Time) (bool, error)
	CaptureAMLBacklog(ctx context.Context, limit int) (int, error)
	CaptureNextAMLInventoryAllocation(ctx context.Context) (bool, error)
	CloseNextExpiredAMLBatch(ctx context.Context) (bool, error)
	OpenNextReadyMerchantPoolBatch(ctx context.Context) (bool, error)
	PublishNextAMLRiskEvent(ctx context.Context) (bool, error)
	PublishNextAMLWithdrawalEvent(ctx context.Context) (bool, error)
	QueueNextAMLPoolActivation(ctx context.Context) (bool, error)
	RebuildNextAMLRiskProjection(ctx context.Context, now time.Time) (bool, error)
	ReconcileNextMerchantPool(ctx context.Context) (bool, int, error)
	ReleaseNextMerchantPoolBaselineHold(ctx context.Context) (bool, error)
}
type AMLCycleRunner interface {
	RunOnce(context.Context) (bool, error)
}
type AMLMaintenanceCycle struct {
	repo      AMLMaintenanceRepository
	jobs      AMLCycleRunner
	preflight AMLCycleRunner
	verify    func(context.Context) error
	log       *logger.Logger
	now       func() time.Time
}

func NewAMLMaintenanceCycle(repo AMLMaintenanceRepository, jobs, preflight AMLCycleRunner, verify func(context.Context) error, log *logger.Logger, now func() time.Time) *AMLMaintenanceCycle {
	if log == nil {
		log = logger.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &AMLMaintenanceCycle{repo, jobs, preflight, verify, log, now}
}

// Run advances bounded durable work in the required order. Polling and shutdown
// belong to the worker runtime; each operation retains its own atomic SQL fence.
func (s *AMLMaintenanceCycle) Run(ctx context.Context) {

	for range 5 {
		progressed, err := s.preflight.RunOnce(ctx)
		if err != nil {
			s.log.Warnw("AML automatic baseline deferred", "error", err)
			break
		}
		if !progressed {
			break
		}
	}
	for range 10 {
		progressed, _, err := s.repo.ReconcileNextMerchantPool(ctx)
		if err != nil {
			s.log.Warnw("AML merchant pool planning failed", "error", err)
		}
		if !progressed {
			break
		}
	}
	for range 100 {
		changed, err := s.repo.CloseNextExpiredAMLBatch(ctx)
		if err != nil {
			s.log.Warnw("AML batch expiry failed", "error", err)
			break
		}
		if !changed {
			break
		}
	}
	if s.verify != nil {
		if err := s.verify(ctx); err != nil {
			s.log.Warnw("AML internal proof recovery failed", "error", err)
		}
	}
	if _, err := s.repo.CaptureAMLBacklog(ctx, 100); err != nil {
		s.log.Warnw("AML proof backlog failed", "error", err)
		return
	}
	for range 100 {
		progressed, err := s.repo.CaptureNextAMLInventoryAllocation(ctx)
		if err != nil {
			s.log.Warnw("AML inventory allocation failed", "error", err)
			break
		}
		if !progressed {
			break
		}
	}
	for range 20 {
		claimed, err := s.jobs.RunOnce(ctx)
		if err != nil {
			s.log.Warnw("AML job execution failed", "error", err)
			break
		}
		if !claimed {
			break
		}
		if ctx.Err() != nil {
			return
		}
	}
	for range 100 {
		progressed, err := s.repo.ApplyNextAMLDecision(ctx, s.now().UTC())
		if err != nil {
			s.log.Warnw("AML decision application failed", "error", err)
			break
		}
		if !progressed {
			break
		}
	}
	for range 100 {
		progressed, err := s.repo.RebuildNextAMLRiskProjection(ctx, s.now().UTC())
		if err != nil {
			s.log.Warnw("AML risk projection failed", "error", err)
			break
		}
		if !progressed {
			break
		}
	}
	for range 100 {
		progressed, err := s.repo.PublishNextAMLRiskEvent(ctx)
		if err != nil {
			s.log.Warnw("AML risk event publication failed", "error", err)
			break
		}
		if !progressed {
			break
		}
	}
	for range 100 {
		progressed, err := s.repo.PublishNextAMLWithdrawalEvent(ctx)
		if err != nil {
			s.log.Warnw("AML withdrawal event publication failed", "error", err)
			break
		}
		if !progressed {
			break
		}
	}
	for range 10 {
		progressed, err := s.repo.ReleaseNextMerchantPoolBaselineHold(ctx)
		if err != nil {
			s.log.Warnw("AML merchant pool baseline preflight failed", "error", err)
		}
		if !progressed {
			break
		}
	}
	for range 10 {
		progressed, err := s.repo.OpenNextReadyMerchantPoolBatch(ctx)
		if err != nil {
			s.log.Warnw("AML merchant pool batch opening failed", "error", err)
		}
		if !progressed {
			break
		}
	}
	for range 10 {
		progressed, err := s.repo.QueueNextAMLPoolActivation(ctx)
		if err != nil {
			s.log.Warnw("AML automatic activation deferred", "error", err)
			break
		}
		if !progressed {
			break
		}
	}
}
