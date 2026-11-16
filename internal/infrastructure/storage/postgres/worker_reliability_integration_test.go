//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/automation"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain/automations"
)

type reliabilityHandler func(context.Context, *OutboxMessage) error

func (f reliabilityHandler) Handle(ctx context.Context, msg *OutboxMessage) error { return f(ctx, msg) }

func reliabilityPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return tenant.WithTxManager(ctx, NewTxManagerFromRawPool(pool)), pool
}

func insertReliabilityMessage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, aggregate string) id.ID {
	t.Helper()
	messageID := id.New()
	_, err := pool.Exec(ctx, `INSERT INTO sys_outbox(id,aggregate_type,aggregate_id,event_type,payload,created_at)
 VALUES($1,$2,$1,'test','{}',clock_timestamp()-INTERVAL '1 day')`, messageID, aggregate)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM sys_outbox WHERE id=$1`, messageID) })
	return messageID
}

func TestWorkerReliabilityOutboxOwnershipAndIsolation(t *testing.T) {
	ctx, pool := reliabilityPool(t)
	externalClass, internalClass := "External-"+id.Compact(id.New()), "Internal-"+id.Compact(id.New())
	externalID := insertReliabilityMessage(t, ctx, pool, externalClass)
	insertReliabilityMessage(t, ctx, pool, internalClass)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	handler := reliabilityHandler(func(ctx context.Context, _ *OutboxMessage) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	relay := NewOutboxRelay(pool, 1, handler).FilterAggregates([]string{externalClass}, nil)
	go func() { _, err := relay.ProcessBatch(ctx); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("external handler did not start")
	}
	var oldToken id.ID
	require.NoError(t, pool.QueryRow(ctx, `SELECT claim_token FROM sys_outbox WHERE id=$1`, externalID).Scan(&oldToken))

	// Event age is unrelated to ownership: an old event with a live claim stays owned.
	_, err := relay.RecoverStuck(ctx, time.Second)
	require.NoError(t, err)
	competing := NewOutboxRelay(pool, 1, reliabilityHandler(func(context.Context, *OutboxMessage) error {
		return errors.New("competing worker must not handle a live claim")
	})).FilterAggregates([]string{externalClass}, nil)
	count, err := competing.ProcessBatch(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	internal := NewOutboxRelay(pool, 1, reliabilityHandler(func(context.Context, *OutboxMessage) error { return nil })).FilterAggregates([]string{internalClass}, nil)
	count, err = internal.ProcessBatch(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "internal events progress while external delivery is blocked")

	// Simulate a lost owner followed by a new replica acquiring the expired claim.
	_, err = pool.Exec(ctx, `UPDATE sys_outbox SET lease_until=clock_timestamp()-INTERVAL '1 second' WHERE id=$1`, externalID)
	require.NoError(t, err)
	_, err = relay.RecoverStuck(ctx, time.Second)
	require.NoError(t, err)
	newStarted, newRelease, newDone := make(chan id.ID, 1), make(chan struct{}), make(chan error, 1)
	newRelay := NewOutboxRelay(pool, 1, reliabilityHandler(func(ctx context.Context, msg *OutboxMessage) error {
		newStarted <- msg.ClaimToken
		select {
		case <-newRelease:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})).FilterAggregates([]string{externalClass}, nil)
	go func() { _, err := newRelay.ProcessBatch(ctx); newDone <- err }()
	newToken := <-newStarted
	require.NotEqual(t, oldToken, newToken)
	close(release)
	require.ErrorIs(t, <-done, workerjob.ErrLeaseLost)
	var persistedToken id.ID
	require.NoError(t, pool.QueryRow(ctx, `SELECT claim_token FROM sys_outbox WHERE id=$1 AND status='processing'`, externalID).Scan(&persistedToken))
	require.Equal(t, newToken, persistedToken)
	close(newRelease)
	require.NoError(t, <-newDone)
}

func TestWorkerReliabilityOutboxReturnsHandlerErrors(t *testing.T) {
	ctx, pool := reliabilityPool(t)
	aggregate := "Failure-" + id.Compact(id.New())
	messageID := insertReliabilityMessage(t, ctx, pool, aggregate)
	failed := errors.New("temporary adapter failure")
	relay := NewOutboxRelay(pool, 1, reliabilityHandler(func(context.Context, *OutboxMessage) error { return failed })).FilterAggregates([]string{aggregate}, nil)
	count, err := relay.ProcessBatch(ctx)
	require.ErrorIs(t, err, failed)
	require.Zero(t, count)
	var status string
	var retry int
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,retry_count FROM sys_outbox WHERE id=$1 AND claim_token IS NULL`, messageID).Scan(&status, &retry))
	require.Equal(t, "pending", status)
	require.Equal(t, 1, retry)
}

type transientReliabilityAdapter struct{ calls int }

func (a *transientReliabilityAdapter) Deliver(context.Context, map[string]any, map[string]any, []byte, string, []automation.Attachment) error {
	a.calls++
	if a.calls == 1 {
		return errors.New("temporary adapter failure")
	}
	return nil
}

func TestWorkerReliabilityFailedDeliveryDoesNotSuppressRetryWithCooldown(t *testing.T) {
	ctx, pool := reliabilityPool(t)
	ruleID, eventType := id.New(), "retry-"+id.Compact(id.New())
	_, err := pool.Exec(ctx, `INSERT INTO sys_automation_rules(id,name,event_type,cooldown_seconds,action_template)
 VALUES($1,'retry cooldown',$2,3600,'test')`, ruleID, eventType)
	require.NoError(t, err)
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM sys_automation_rules WHERE id=$1`, ruleID) }()
	_, err = pool.Exec(ctx, `INSERT INTO sys_automation_subscribers(rule_id,subscriber_type,doc_field_path) VALUES($1,'doc_field','doc.createdBy')`, ruleID)
	require.NoError(t, err)
	adapter := &transientReliabilityAdapter{}
	engine, err := automation.NewEngine(NewAutomationRuleRepo(), nil, nil, nil, nil, map[string]automation.Adapter{"internal_notification": adapter}, nil)
	require.NoError(t, err)
	require.Error(t, engine.HandleEvent(ctx, eventType, map[string]any{}))
	require.NoError(t, engine.HandleEvent(ctx, eventType, map[string]any{}))
	require.Equal(t, 2, adapter.calls)
	// A new event immediately after successful delivery is still throttled.
	require.NoError(t, engine.HandleEvent(ctx, eventType, map[string]any{}))
	require.Equal(t, 2, adapter.calls)
}

func TestWorkerReliabilityScheduledRunPersistence(t *testing.T) {
	ctx, pool := reliabilityPool(t)
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse("0 * * * * *")
	require.NoError(t, err)
	for _, policy := range []string{"latest", "all", "skip"} {
		t.Run(policy, func(t *testing.T) {
			rule := automations.Rule{ID: id.New(), Name: "Reliability", EventType: "cron:0 * * * * *", MaxRetries: 2}
			_, err := pool.Exec(ctx, `INSERT INTO sys_automation_rules(id,name,trigger_type,event_type) VALUES($1,$2,'scheduled',$3)`, rule.ID, rule.Name, rule.EventType)
			require.NoError(t, err)
			defer func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM sys_automation_rules WHERE id=$1`, rule.ID)
			}()
			now := time.Now().UTC().Truncate(time.Minute)
			first := now.Add(-3 * time.Minute)
			_, err = pool.Exec(ctx, `INSERT INTO sys_automation_schedules(rule_id,expression,next_run_at,catch_up_policy) VALUES($1,$2,$3,$4)`, rule.ID, rule.EventType, first, policy)
			require.NoError(t, err)
			repo := NewAutomationRunRepo()
			var wg sync.WaitGroup
			errorsCh := make(chan error, 2)
			for range 2 {
				wg.Go(func() { errorsCh <- repo.EnqueueDue(ctx, rule, schedule, now.Add(10*time.Second)) })
			}
			wg.Wait()
			close(errorsCh)
			for err := range errorsCh {
				require.NoError(t, err)
			}
			var count int
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM sys_automation_runs WHERE rule_id=$1`, rule.ID).Scan(&count))
			want := map[string]int{"latest": 1, "all": 4, "skip": 0}[policy]
			require.Equal(t, want, count)
			if want == 0 {
				return
			}
			claimed := make(chan []automations.ScheduledRun, 2)
			errorsCh = make(chan error, 2)
			for range 2 {
				wg.Go(func() { runs, err := repo.Claim(ctx, 1); claimed <- runs; errorsCh <- err })
			}
			wg.Wait()
			close(claimed)
			close(errorsCh)
			for err := range errorsCh {
				require.NoError(t, err)
			}
			var runs []automations.ScheduledRun
			for result := range claimed {
				runs = append(runs, result...)
			}
			require.Len(t, runs, 1, "replicas share one execution slot per rule")
			old := runs[0]
			if policy == "latest" {
				require.True(t, old.ScheduledFor.Equal(now))
			}
			_, err = pool.Exec(ctx, `UPDATE sys_automation_runs SET lease_until=clock_timestamp()-INTERVAL '1 second' WHERE id=$1`, old.ID)
			require.NoError(t, err)
			reclaimed, err := repo.Claim(ctx, 1)
			require.NoError(t, err)
			require.Len(t, reclaimed, 1)
			require.Equal(t, old.ID, reclaimed[0].ID)
			require.NotEqual(t, old.ClaimToken, reclaimed[0].ClaimToken)
			require.ErrorIs(t, repo.Renew(ctx, old), workerjob.ErrLeaseLost)
			require.ErrorIs(t, repo.Finish(ctx, old, "succeeded", nil), workerjob.ErrLeaseLost)
			require.NoError(t, repo.Finish(ctx, reclaimed[0], "succeeded", nil))
		})
	}
}
