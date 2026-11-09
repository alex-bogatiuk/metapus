package automation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

type schedulerRuleRepo struct {
	automations.RuleRepository
	mu   sync.Mutex
	rule automations.Rule
}

func (r *schedulerRuleRepo) GetByID(context.Context, id.ID) (*automations.Rule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.rule
	copy.Subscribers = append([]automations.Subscriber(nil), copy.Subscribers...)
	return &copy, nil
}
func (r *schedulerRuleRepo) ListActiveByTriggerType(ctx context.Context, _ automations.TriggerType) ([]automations.Rule, error) {
	rule, _ := r.GetByID(ctx, id.Nil())
	return []automations.Rule{*rule}, nil
}
func (r *schedulerRuleRepo) update(fn func(*automations.Rule)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.rule)
}

type schedulerExecutor struct {
	calls    chan automations.Rule
	release  <-chan struct{}
	canceled chan struct{}
}

func (e *schedulerExecutor) HandleScheduledRule(ctx context.Context, rule automations.Rule, _ map[string]any) error {
	e.calls <- rule
	if e.canceled != nil {
		<-ctx.Done()
		close(e.canceled)
	}
	if e.release != nil {
		<-e.release
	}
	return nil
}
func (e *schedulerExecutor) HandleScheduledReportRule(ctx context.Context, rule automations.Rule, _ time.Time) error {
	return e.HandleScheduledRule(ctx, rule, nil)
}

type memoryScheduledRuns struct {
	mu          sync.Mutex
	next        map[id.ID]time.Time
	expressions map[id.ID]string
	enqueued    []automations.Rule
	queue       []automations.ScheduledRun
	running     map[id.ID]bool
}

func newMemoryScheduledRuns() *memoryScheduledRuns {
	return &memoryScheduledRuns{next: make(map[id.ID]time.Time), expressions: make(map[id.ID]string), running: make(map[id.ID]bool)}
}
func (r *memoryScheduledRuns) EnqueueDue(_ context.Context, rule automations.Rule, schedule automations.Schedule, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueued = append(r.enqueued, rule)
	next, exists := r.next[rule.ID]
	if !exists || r.expressions[rule.ID] != rule.EventType {
		r.next[rule.ID] = schedule.Next(now)
		r.expressions[rule.ID] = rule.EventType
		return nil
	}
	if !next.After(now) {
		due := automations.LatestDue(schedule, next, now)
		r.queue = append(r.queue, automations.ScheduledRun{ID: id.New(), RuleID: rule.ID, Expression: rule.EventType, ScheduledFor: due, ClaimToken: id.New(), MaxAttempts: 4})
		r.next[rule.ID] = schedule.Next(due)
	}
	return nil
}
func (r *memoryScheduledRuns) Claim(context.Context, int) ([]automations.ScheduledRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, run := range r.queue {
		if !r.running[run.RuleID] {
			r.queue = append(r.queue[:i], r.queue[i+1:]...)
			r.running[run.RuleID] = true
			return []automations.ScheduledRun{run}, nil
		}
	}
	return nil, nil
}
func (*memoryScheduledRuns) Renew(context.Context, automations.ScheduledRun) error { return nil }
func (r *memoryScheduledRuns) Finish(_ context.Context, run automations.ScheduledRun, _ string, _ error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, run.RuleID)
	return nil
}
func newSchedulerFixture() (*Scheduler, *schedulerRuleRepo, *schedulerExecutor, *memoryScheduledRuns) {
	r := &schedulerRuleRepo{rule: automations.Rule{ID: id.New(), Name: "initial", IsActive: true, TriggerType: automations.TriggerScheduled, EventType: "cron:0 * * * * *"}}
	e := &schedulerExecutor{calls: make(chan automations.Rule, 8)}
	runs := newMemoryScheduledRuns()
	return NewScheduler(e, r, runs), r, e, runs
}

func TestSchedulerReloadsConfigurationAndFencesObsoleteCallbacks(t *testing.T) {
	s, r, _, runs := newSchedulerFixture()
	ctx := context.Background()
	s.Refresh(ctx)
	job := s.jobs[r.rule.ID.String()]
	r.update(func(rule *automations.Rule) {
		rule.Name = "updated"
		rule.ActionTemplate = "new body"
		rule.Subscribers = []automations.Subscriber{{}}
	})
	s.invoke(r.rule.ID, job.generation, job.expression)
	require.Equal(t, "updated", runs.enqueued[len(runs.enqueued)-1].Name)
	s.Refresh(ctx)
	next := s.jobs[r.rule.ID.String()]
	require.Equal(t, job.entry, next.entry)
	require.NotEqual(t, job.fingerprint, next.fingerprint)
	r.update(func(rule *automations.Rule) { rule.ExecutionCount++; rule.Version++; rule.UpdatedAt = time.Now() })
	s.Refresh(ctx)
	require.Equal(t, next.fingerprint, s.jobs[r.rule.ID.String()].fingerprint)
	r.update(func(rule *automations.Rule) { rule.EventType = "cron:0 1 * * * *" })
	count := len(runs.enqueued)
	s.invoke(r.rule.ID, job.generation, job.expression)
	require.Len(t, runs.enqueued, count)
	s.Refresh(ctx)
	count = len(runs.enqueued)
	s.invoke(r.rule.ID, job.generation, job.expression)
	require.Len(t, runs.enqueued, count)
	r.update(func(rule *automations.Rule) { rule.IsActive = false })
	next = s.jobs[r.rule.ID.String()]
	s.invoke(r.rule.ID, next.generation, next.expression)
	require.Len(t, runs.enqueued, count)
	s.Refresh(ctx)
	require.Zero(t, s.JobCount())
}

func TestSchedulerDoesNotOverlapPersistedRunsAcrossScheduleReplacement(t *testing.T) {
	s, r, e, runs := newSchedulerFixture()
	release := make(chan struct{})
	e.release = release
	run := automations.ScheduledRun{ID: id.New(), RuleID: r.rule.ID, Expression: r.rule.EventType, ScheduledFor: time.Now()}
	runs.queue = append(runs.queue, run)
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.processPending(context.Background()) }()
	<-e.calls
	r.update(func(rule *automations.Rule) { rule.EventType = "cron:0 1 * * * *" })
	runs.mu.Lock()
	run.ID = id.New()
	run.Expression = r.rule.EventType
	runs.queue = append(runs.queue, run)
	runs.mu.Unlock()
	_, processErr := s.processPending(context.Background())
	require.NoError(t, processErr)
	require.Empty(t, e.calls)
	close(release)
	<-done
	_, processErr = s.processPending(context.Background())
	require.NoError(t, processErr)
	require.Len(t, e.calls, 1)
}

func TestSchedulerShutdownDrainsPersistedExecutors(t *testing.T) {
	s, r, e, _ := newSchedulerFixture()
	r.rule.EventType = "cron:* * * * * *"
	release := make(chan struct{})
	e.release = release
	e.canceled = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.Start(ctx) }()
	select {
	case <-e.calls:
	case <-time.After(5 * time.Second):
		t.Fatal("persisted executor did not start")
	}
	cancel()
	<-e.canceled
	select {
	case <-done:
		t.Fatal("scheduler returned with an active executor")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not drain")
	}
}
