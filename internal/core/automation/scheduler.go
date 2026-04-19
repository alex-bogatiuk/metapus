package automation

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"metapus/internal/domain/automations"
	"metapus/pkg/logger"
)

// Scheduler manages CRON-based scheduled automation rules.
// It polls the DB for active scheduled rules and dynamically adds/removes cron jobs.
type Scheduler struct {
	cron      *cron.Cron
	engine    *Engine
	ruleRepo  automations.RuleRepository
	mu        sync.Mutex
	jobs      map[string]cron.EntryID // ruleID → cron entry
	parentCtx context.Context         // enriched context with Pool/TxManager (for derived timeouts)
}

// NewScheduler creates a new automation scheduler.
func NewScheduler(engine *Engine, ruleRepo automations.RuleRepository) *Scheduler {
	return &Scheduler{
		cron:     cron.New(cron.WithSeconds()), // 6-field cron (sec min hr dom mon dow)
		engine:   engine,
		ruleRepo: ruleRepo,
		jobs:     make(map[string]cron.EntryID),
	}
}
// refreshInterval is how often the scheduler re-syncs rules from DB.
const refreshInterval = 60 * time.Second

// Start begins the scheduler. Should be called once per tenant worker.
// ctx MUST contain Pool and TxManager (enriched by runTenantWorker).
func (s *Scheduler) Start(ctx context.Context) {
	s.parentCtx = ctx // Store enriched context for cron callbacks and periodic refresh

	s.Refresh(ctx)
	s.cron.Start()

	logger.Info(ctx, "automation scheduler started", "jobCount", len(s.jobs))

	// Periodic refresh to pick up rule changes without restart
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.cron.Stop()
			logger.Info(ctx, "automation scheduler stopped")
			return
		case <-ticker.C:
			// Derive from parent ctx (not context.Background) to preserve Pool/TxManager.
			refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			s.Refresh(refreshCtx)
			cancel()
		}
	}
}

// Refresh reloads scheduled rules from DB and syncs cron jobs.
// Called on startup and can be called periodically.
func (s *Scheduler) Refresh(ctx context.Context) {
	rules, err := s.ruleRepo.ListActiveByTriggerType(ctx, automations.TriggerScheduled)
	if err != nil {
		logger.Error(ctx, "scheduler: failed to load scheduled rules", "error", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build a set of current rule IDs
	activeRules := make(map[string]automations.Rule, len(rules))
	for _, r := range rules {
		activeRules[r.ID.String()] = r
	}

	// Remove jobs for rules that are no longer active
	for ruleID, entryID := range s.jobs {
		if _, exists := activeRules[ruleID]; !exists {
			s.cron.Remove(entryID)
			delete(s.jobs, ruleID)
			logger.Debug(ctx, "scheduler: removed job for deactivated rule", "ruleId", ruleID)
		}
	}

	// Add/update jobs for active rules
	for _, rule := range rules {
		ruleIDStr := rule.ID.String()
		if _, exists := s.jobs[ruleIDStr]; exists {
			continue // Already scheduled
		}

		// event_type stores the CRON expression for scheduled rules (format: "cron:<expr>")
		cronExpr := rule.EventType
		if strings.HasPrefix(cronExpr, "cron:") {
			cronExpr = strings.TrimPrefix(cronExpr, "cron:")
		}
		if cronExpr == "" {
			logger.Warn(ctx, "scheduler: scheduled rule has empty cron expression", "ruleId", rule.ID)
			continue
		}

		// Capture rule for closure
		capturedRule := rule
		entryID, err := s.cron.AddFunc(cronExpr, func() {
			// Derive from parentCtx (not context.Background) to preserve Pool/TxManager.
			// WithTimeout ensures the cron job doesn't hang indefinitely.
			execCtx, cancel := context.WithTimeout(s.parentCtx, 30*time.Second)
			defer cancel()
			s.executeScheduledRule(execCtx, capturedRule)
		})
		if err != nil {
			logger.Error(ctx, "scheduler: invalid cron expression",
				"ruleId", rule.ID, "cron", cronExpr, "error", err)
			continue
		}

		s.jobs[ruleIDStr] = entryID
		logger.Debug(ctx, "scheduler: added cron job", "ruleId", rule.ID, "cron", cronExpr)
	}
}

// executeScheduledRule builds a synthetic payload and calls Engine.HandleScheduledRule.
// Uses HandleScheduledRule (not HandleEvent) because scheduled rules store their CRON
// expression in event_type, which is not a matchable event string.
func (s *Scheduler) executeScheduledRule(ctx context.Context, rule automations.Rule) {
	now := time.Now()
	payload := map[string]any{
		"action":     "scheduled",
		"entityType": "automation",
		"doc": map[string]any{
			"ruleName":  rule.Name,
			"ruleId":    rule.ID.String(),
			"timestamp": now.Format(time.RFC3339),
			"date":      now.Format("02.01.2006"),
			"time":      now.Format("15:04:05"),
		},
	}

	if err := s.engine.HandleScheduledRule(ctx, rule, payload); err != nil {
		logger.Error(ctx, "scheduler: rule execution failed", "ruleId", rule.ID, "error", err)
	}
}

// JobCount returns the number of active cron jobs.
func (s *Scheduler) JobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}
