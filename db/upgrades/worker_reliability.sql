-- Apply to each existing tenant DB with all old worker replicas stopped.
-- This operation keeps business data and the existing goose migration versions.
BEGIN;
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE sys_outbox ADD COLUMN IF NOT EXISTS claim_token UUID;
ALTER TABLE sys_outbox ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE sys_outbox ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;
-- Legacy claims have no owner token. Stopped workers can no longer acknowledge them.
UPDATE sys_outbox SET status='pending',claim_token=NULL,claimed_at=NULL,lease_until=NULL
 WHERE status='processing' AND claim_token IS NULL;
DROP INDEX IF EXISTS idx_outbox_stuck;
CREATE INDEX idx_outbox_stuck ON sys_outbox(lease_until) WHERE status='processing';
ALTER TABLE sys_webhook_delivery_jobs ADD COLUMN IF NOT EXISTS claim_token UUID;
ALTER TABLE sys_webhook_delivery_jobs ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;
UPDATE sys_webhook_delivery_jobs SET status='pending',claim_token=NULL,lease_until=NULL,updated_at=clock_timestamp()
 WHERE status='processing' AND claim_token IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_webhook_delivery_jobs_processing_merchant
 ON sys_webhook_delivery_jobs(merchant_id) WHERE status='processing';
CREATE INDEX IF NOT EXISTS idx_webhook_delivery_jobs_expired
 ON sys_webhook_delivery_jobs(lease_until) WHERE status='processing';
CREATE INDEX IF NOT EXISTS idx_webhook_delivery_jobs_last_attempt
 ON sys_webhook_delivery_jobs(merchant_id,updated_at DESC) WHERE attempt_count>0;
CREATE TABLE IF NOT EXISTS sys_automation_schedules (
 rule_id UUID PRIMARY KEY REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
 expression TEXT NOT NULL,
 next_run_at TIMESTAMPTZ NOT NULL,
 catch_up_policy TEXT NOT NULL DEFAULT 'latest' CHECK(catch_up_policy IN ('latest','all','skip')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 _deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS sys_automation_runs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 rule_id UUID NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
 expression TEXT NOT NULL,
 scheduled_for TIMESTAMPTZ NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','succeeded','failed','skipped')),
 attempt INT NOT NULL DEFAULT 0,
 max_attempts INT NOT NULL DEFAULT 4 CHECK(max_attempts>0),
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 claim_token UUID,
 lease_until TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 _deleted_at TIMESTAMPTZ,
 UNIQUE(rule_id,scheduled_for)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_automation_runs_processing_rule ON sys_automation_runs(rule_id) WHERE status='processing';
CREATE INDEX IF NOT EXISTS idx_automation_runs_due ON sys_automation_runs(next_attempt_at,scheduled_for) WHERE status='pending';
CREATE INDEX IF NOT EXISTS idx_automation_runs_expired ON sys_automation_runs(lease_until) WHERE status='processing';
CREATE OR REPLACE TRIGGER trg_automation_schedules_txid BEFORE INSERT OR UPDATE ON sys_automation_schedules
 FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE OR REPLACE TRIGGER trg_automation_runs_txid BEFORE INSERT OR UPDATE ON sys_automation_runs
 FOR EACH ROW EXECUTE FUNCTION update_txid_column();


COMMIT;
