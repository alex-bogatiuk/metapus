package postgres

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain/automations"
)

type AutomationRunRepo struct{}

func NewAutomationRunRepo() *AutomationRunRepo { return &AutomationRunRepo{} }

func (r *AutomationRunRepo) EnqueueDue(ctx context.Context, rule automations.Rule, schedule automations.Schedule, now time.Time) error {
	txm := MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		expression := rule.EventType
		var enabled bool
		if err := q.QueryRow(txCtx, `SELECT is_active AND NOT deletion_mark AND trigger_type='scheduled' AND event_type=$2
 FROM sys_automation_rules WHERE id=$1 FOR SHARE`, rule.ID, expression).Scan(&enabled); err != nil {
			return err
		}
		if !enabled {
			return nil
		}
		initial := schedule.Next(now)
		if initial.IsZero() {
			return fmt.Errorf("schedule has no next occurrence")
		}
		if _, err := q.Exec(txCtx, `INSERT INTO sys_automation_schedules(rule_id,expression,next_run_at)
 VALUES($1,$2,$3) ON CONFLICT(rule_id) DO NOTHING`, rule.ID, expression, initial); err != nil {
			return err
		}
		var previous, policy string
		var next time.Time
		var disabledAt *time.Time
		if err := q.QueryRow(txCtx, `SELECT expression,next_run_at,catch_up_policy,_deleted_at
 FROM sys_automation_schedules WHERE rule_id=$1 FOR UPDATE`, rule.ID).Scan(&previous, &next, &policy, &disabledAt); err != nil {
			return err
		}
		if previous != expression || disabledAt != nil {
			_, err := q.Exec(txCtx, `UPDATE sys_automation_schedules SET expression=$2,next_run_at=$3,_deleted_at=NULL,updated_at=clock_timestamp() WHERE rule_id=$1`, rule.ID, expression, initial)
			return err
		}
		if next.After(now) {
			return nil
		}
		var due []time.Time
		switch policy {
		case "skip":
			// A live callback is on time within the scheduler's one-second tick.
			if now.Sub(next) < 2*time.Second {
				due = append(due, next)
			}
			next = initial
		case "latest":
			latest := automations.LatestDue(schedule, next, now)
			due = append(due, latest)
			next = schedule.Next(latest)
		case "all":
			for len(due) < 100 && !next.After(now) {
				due = append(due, next)
				next = schedule.Next(next)
				if next.IsZero() {
					return fmt.Errorf("schedule has no next occurrence")
				}
			}
		default:
			return fmt.Errorf("unknown schedule catch-up policy %q", policy)
		}
		for _, scheduledFor := range due {
			if _, err := q.Exec(txCtx, `INSERT INTO sys_automation_runs(rule_id,expression,scheduled_for,max_attempts)
 VALUES($1,$2,$3,$4) ON CONFLICT(rule_id,scheduled_for) DO NOTHING`, rule.ID, expression, scheduledFor, max(1, rule.MaxRetries+1)); err != nil {
				return err
			}
		}
		_, err := q.Exec(txCtx, `UPDATE sys_automation_schedules SET next_run_at=$2,updated_at=clock_timestamp() WHERE rule_id=$1`, rule.ID, next)
		return err
	})
}

func (r *AutomationRunRepo) Claim(ctx context.Context, limit int) ([]automations.ScheduledRun, error) {
	q := MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `UPDATE sys_automation_runs SET
 status=CASE WHEN attempt>=max_attempts THEN 'failed' ELSE 'pending' END,
 claim_token=NULL,lease_until=NULL,last_error='executor lease expired',updated_at=clock_timestamp()
 WHERE status='processing' AND lease_until<=clock_timestamp()`); err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, `UPDATE sys_automation_runs j SET status='skipped',updated_at=clock_timestamp()
 FROM sys_automation_rules r WHERE j.rule_id=r.id AND j.status='pending'
 AND (NOT r.is_active OR r.deletion_mark OR r.trigger_type<>'scheduled' OR r.event_type<>j.expression)`); err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, `UPDATE sys_automation_schedules s SET _deleted_at=clock_timestamp()
 FROM sys_automation_rules r WHERE s.rule_id=r.id AND s._deleted_at IS NULL
 AND (NOT r.is_active OR r.deletion_mark OR r.trigger_type<>'scheduled')`); err != nil {
		return nil, err
	}
	var runs []automations.ScheduledRun
	txm := MustGetTxManager(ctx)
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		runs = nil
		q := txm.GetQuerier(txCtx)
		// The rule row lock is the per-rule execution slot. It must be taken in
		// its own statement: a claimer whose snapshot predates a competitor's
		// commit can still win the lock after that commit, and would then miss
		// the competitor's new processing run and collide with
		// uq_automation_runs_processing_rule. The claim below runs with a fresh
		// READ COMMITTED snapshot taken after the lock, so it sees that run.
		ruleRows, err := q.Query(txCtx, `SELECT r.id FROM sys_automation_rules r
 WHERE r.is_active AND NOT r.deletion_mark AND r.trigger_type='scheduled'
 AND EXISTS(SELECT 1 FROM sys_automation_runs j WHERE j.rule_id=r.id AND j.status='pending' AND j.next_attempt_at<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM sys_automation_runs j WHERE j.rule_id=r.id AND j.status='processing')
 ORDER BY r.last_executed_at NULLS FIRST,r.id LIMIT $1 FOR UPDATE OF r SKIP LOCKED`, max(1, limit))
		if err != nil {
			return err
		}
		var ruleIDs []id.ID
		for ruleRows.Next() {
			var ruleID id.ID
			if err := ruleRows.Scan(&ruleID); err != nil {
				ruleRows.Close()
				return err
			}
			ruleIDs = append(ruleIDs, ruleID)
		}
		ruleRows.Close()
		if err := ruleRows.Err(); err != nil || len(ruleIDs) == 0 {
			return err
		}
		rows, err := q.Query(txCtx, `WITH due AS (
 SELECT candidate.id FROM unnest($1::uuid[]) AS r(id) CROSS JOIN LATERAL (
 SELECT j.id FROM sys_automation_runs j WHERE j.rule_id=r.id AND j.status='pending'
 AND j.next_attempt_at<=clock_timestamp() ORDER BY j.scheduled_for LIMIT 1 FOR UPDATE OF j SKIP LOCKED
 ) candidate
 WHERE NOT EXISTS(SELECT 1 FROM sys_automation_runs p WHERE p.rule_id=r.id AND p.status='processing')
 ) UPDATE sys_automation_runs j SET status='processing',attempt=attempt+1,
 claim_token=gen_random_uuid_v7(),lease_until=clock_timestamp()+INTERVAL '2 minutes',updated_at=clock_timestamp()
 FROM due WHERE j.id=due.id RETURNING j.id,j.rule_id,j.expression,j.scheduled_for,j.claim_token,j.attempt,j.max_attempts`, ruleIDs)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var run automations.ScheduledRun
			if err := rows.Scan(&run.ID, &run.RuleID, &run.Expression, &run.ScheduledFor, &run.ClaimToken, &run.Attempt, &run.MaxAttempts); err != nil {
				return err
			}
			runs = append(runs, run)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return runs, nil
}

func (r *AutomationRunRepo) Renew(ctx context.Context, run automations.ScheduledRun) error {
	result, err := MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE sys_automation_runs
 SET lease_until=clock_timestamp()+INTERVAL '2 minutes'
 WHERE id=$1 AND status='processing' AND claim_token=$2 AND lease_until>clock_timestamp()`, run.ID, run.ClaimToken)
	if err == nil && result.RowsAffected() != 1 {
		return workerjob.ErrLeaseLost
	}
	return err
}

func (r *AutomationRunRepo) Finish(ctx context.Context, run automations.ScheduledRun, status string, workErr error) error {
	message := ""
	if workErr != nil {
		message = workErr.Error()
		status = "pending"
		if run.Attempt >= run.MaxAttempts {
			status = "failed"
		}
	}
	delay := time.Second * time.Duration(1<<min(run.Attempt, 8))
	result, err := MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE sys_automation_runs
 SET status=$3,last_error=$4,next_attempt_at=clock_timestamp()+($5*INTERVAL '1 second'),
 claim_token=NULL,lease_until=NULL,updated_at=clock_timestamp()
 WHERE id=$1 AND status='processing' AND claim_token=$2 AND lease_until>clock_timestamp()`, run.ID, run.ClaimToken, status, message, delay.Seconds())
	if err == nil && result.RowsAffected() != 1 {
		return workerjob.ErrLeaseLost
	}
	return err
}

var _ automations.ScheduledRunRepository = (*AutomationRunRepo)(nil)
