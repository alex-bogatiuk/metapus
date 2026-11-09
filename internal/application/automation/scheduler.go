package automation

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain/automations"
	"metapus/pkg/logger"
)

type ScheduledRuleExecutor interface {
	HandleScheduledRule(context.Context, automations.Rule, map[string]any) error
	HandleScheduledReportRule(context.Context, automations.Rule, time.Time) error
}

type scheduledJob struct {
	entry       cron.EntryID
	generation  id.ID
	expression  string
	fingerprint [32]byte
	schedule    cron.Schedule
}

// Scheduler reloads the rule at invocation and prevents overlap across job replacements.
type Scheduler struct {
	cron      *cron.Cron
	engine    ScheduledRuleExecutor
	ruleRepo  automations.RuleRepository
	runRepo   automations.ScheduledRunRepository
	mu        sync.Mutex
	jobs      map[string]scheduledJob
	running   map[string]bool
	stopped   bool
	parentCtx context.Context
}

func NewScheduler(engine ScheduledRuleExecutor, ruleRepo automations.RuleRepository, runRepo automations.ScheduledRunRepository) *Scheduler {
	return &Scheduler{cron: cron.New(cron.WithSeconds(), cron.WithLocation(time.UTC)), engine: engine, ruleRepo: ruleRepo, runRepo: runRepo,
		jobs: make(map[string]scheduledJob), running: make(map[string]bool), parentCtx: context.Background()}
}

const _refreshInterval = 60 * time.Second

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.parentCtx = ctx
	s.mu.Unlock()
	s.Refresh(ctx)
	s.cron.Start()
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for ctx.Err() == nil {
				processed, err := s.processPending(ctx)
				if err != nil && ctx.Err() == nil {
					logger.Error(ctx, "scheduler: execute persisted run", "error", err)
				}
				if processed && err == nil {
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
	defer workers.Wait()
	ticker := time.NewTicker(_refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := s.Stop(shutdownCtx); err != nil {
				logger.Error(ctx, "automation scheduler drain timed out", "error", err)
			}
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			s.Refresh(refreshCtx)
			cancel()
		}
	}
}

// Stop waits for callbacks without extending the caller's shutdown budget.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	drained := s.cron.Stop()
	select {
	case <-drained.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ruleExecutionFingerprint(rule automations.Rule) ([32]byte, error) {
	// Statistics and bookkeeping timestamps cannot change executable configuration.
	rule.ExecutionCount, rule.ErrorCount, rule.Version = 0, 0, 0
	rule.LastExecutedAt = nil
	rule.CreatedAt, rule.UpdatedAt = time.Time{}, time.Time{}
	encoded, err := json.Marshal(rule)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func (s *Scheduler) Refresh(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	rules, err := s.ruleRepo.ListActiveByTriggerType(ctx, automations.TriggerScheduled)
	if err != nil {
		logger.Error(ctx, "scheduler: load scheduled rules", "error", err)
		return
	}
	active := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if !rule.IsActive || rule.DeletionMark || rule.TriggerType != automations.TriggerScheduled {
			continue
		}
		key := rule.ID.String()
		active[key] = true
		expression := strings.TrimPrefix(rule.EventType, "cron:")
		fingerprint, err := ruleExecutionFingerprint(rule)
		if err != nil {
			logger.Error(ctx, "scheduler: fingerprint rule", "ruleId", key, "error", err)
			continue
		}
		parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		schedule, parseErr := parser.Parse(expression)
		if parseErr != nil {
			logger.Error(ctx, "scheduler: invalid cron expression", "ruleId", key, "error", parseErr)
			continue
		}
		if err := s.runRepo.EnqueueDue(ctx, rule, schedule, time.Now().UTC()); err != nil {
			logger.Error(ctx, "scheduler: persist calendar occurrences", "ruleId", key, "error", err)
		}
		previous, exists := s.jobs[key]
		if exists && previous.expression == expression {
			previous.fingerprint = fingerprint
			s.jobs[key] = previous
			continue
		}
		if exists {
			s.cron.Remove(previous.entry)
			delete(s.jobs, key)
		}
		generation := id.New()
		ruleID := rule.ID
		entry, err := s.cron.AddFunc(expression, func() { s.invoke(ruleID, generation, expression) })
		if err != nil {
			logger.Error(ctx, "scheduler: invalid cron expression", "ruleId", key, "error", err)
			continue
		}
		s.jobs[key] = scheduledJob{entry: entry, generation: generation, expression: expression, fingerprint: fingerprint, schedule: schedule}
	}
	for key, job := range s.jobs {
		if !active[key] {
			s.cron.Remove(job.entry)
			delete(s.jobs, key)
		}
	}
}

func (s *Scheduler) invoke(ruleID id.ID, generation id.ID, expression string) {
	key := ruleID.String()
	s.mu.Lock()
	job, exists := s.jobs[key]
	if s.stopped || !exists || job.generation != generation || s.running[key] {
		s.mu.Unlock()
		return
	}
	s.running[key] = true
	parentCtx := s.parentCtx
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.running, key); s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()
	// GetByID includes subscribers; this snapshot remains fixed for this invocation.
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		logger.Error(ctx, "scheduler: reload rule", "ruleId", key, "error", err)
		return
	}
	if rule == nil || !rule.IsActive || rule.DeletionMark || rule.TriggerType != automations.TriggerScheduled || strings.TrimPrefix(rule.EventType, "cron:") != expression {
		return
	}
	if ctx.Err() != nil {
		return
	}
	if err := s.runRepo.EnqueueDue(ctx, *rule, job.schedule, time.Now().UTC()); err != nil {
		logger.Error(ctx, "scheduler: enqueue scheduled run", "ruleId", key, "error", err)
	}
}

// processPending claims one occurrence per executor. The durable identity is
// (rule_id, scheduled_for), not the callback's actual wake-up time.
func (s *Scheduler) processPending(ctx context.Context) (bool, error) {
	runs, err := s.runRepo.Claim(ctx, 1)
	if err != nil || len(runs) == 0 {
		return false, err
	}
	run := runs[0]
	status := "succeeded"
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err = workerjob.WithLease(runCtx, 20*time.Second, func(renewCtx context.Context) error { return s.runRepo.Renew(renewCtx, run) }, func(workCtx context.Context) error {
		rule, err := s.ruleRepo.GetByID(workCtx, run.RuleID)
		if err != nil {
			return err
		}
		if rule == nil || !rule.IsActive || rule.DeletionMark || rule.TriggerType != automations.TriggerScheduled || rule.EventType != run.Expression {
			status = "skipped"
			return nil
		}
		if rule.ReactionType == automations.ReactionGenerateReport {
			return s.engine.HandleScheduledReportRule(workCtx, *rule, run.ScheduledFor)
		}
		now := run.ScheduledFor
		payload := map[string]any{"action": "scheduled", "entityType": "automation", "doc": map[string]any{
			"ruleName": rule.Name, "ruleId": rule.ID.String(), "runId": run.ID.String(),
			"timestamp": now.Format(time.RFC3339), "date": now.Format("02.01.2006"), "time": now.Format("15:04:05"),
		}}
		return s.engine.HandleScheduledRule(workCtx, *rule, payload)
	})
	if errors.Is(err, workerjob.ErrLeaseLost) || ctx.Err() != nil {
		return true, err
	}
	return true, errors.Join(err, s.runRepo.Finish(ctx, run, status, err))
}

// JobCount returns the number of active cron jobs.
func (s *Scheduler) JobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}
