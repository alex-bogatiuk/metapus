-- +goose Up
-- sys_automation_rules: event → condition → reaction automation rules.

CREATE TABLE sys_automation_rules (
    id                 UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    name               VARCHAR(255)  NOT NULL,
    description        TEXT,

    -- TRIGGER
    trigger_type       VARCHAR(30)   NOT NULL DEFAULT 'entity_event',
    -- entity_event | business_event | scheduled | incoming_webhook
    event_type         VARCHAR(100)  NOT NULL,
    -- Action: posted, unposted, created, updated, deleted; for scheduled: cron:0 9 * * 1-5
    target_entities    TEXT[],
    -- Array of entity keys: ['crypto_invoice','crypto_payment']. NULL = any entity (wildcard).

    -- CONDITION
    condition_cel      TEXT,

    -- REACTION
    reaction_type      VARCHAR(30)   NOT NULL DEFAULT 'notify' CHECK (reaction_type IN ('notify', 'webhook_call', 'generate_report')),
    -- notify | webhook_call | generate_report
    notif_severity     VARCHAR(16)   NOT NULL DEFAULT 'info' CHECK (notif_severity IN ('info', 'warning', 'error', 'success')),
    message_format     VARCHAR(20)   NOT NULL DEFAULT 'text',
    -- text | markdown | html
    action_template    TEXT          NOT NULL DEFAULT '',

    -- REPORT (only for reaction_type = 'generate_report')
    report_config      JSONB,
    -- { "datasetKey", "variantId", "periodType", "customDays", "extraFilters", "fileFormat", "includeEmptyReport", "timezone" }


    -- SETTINGS
    priority           INT           NOT NULL DEFAULT 0,
    max_retries        INT           NOT NULL DEFAULT 3,
    cooldown_seconds   INT           NOT NULL DEFAULT 0,
    organization_id    UUID          REFERENCES cat_organizations(id) ON DELETE CASCADE,
    is_active          BOOLEAN       NOT NULL DEFAULT TRUE,

    -- STATS (updated atomically by Engine)
    execution_count    INT           NOT NULL DEFAULT 0,
    error_count        INT           NOT NULL DEFAULT 0,
    last_executed_at   TIMESTAMPTZ,

    -- Standard Metapus columns
    deletion_mark      BOOLEAN       NOT NULL DEFAULT FALSE,
    version            INT           NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    _deleted_at        TIMESTAMPTZ,
    _txid              BIGINT        DEFAULT txid_current()
);

-- Active rules by event type — main query for Engine.Evaluate
CREATE INDEX idx_sys_auto_rules_event_type
    ON sys_automation_rules(event_type)
    WHERE is_active = TRUE AND deletion_mark = FALSE;

-- GIN index for array matching: 'crypto_invoice' = ANY(target_entities)
CREATE INDEX idx_sys_auto_rules_target_entities
    ON sys_automation_rules USING GIN(target_entities)
    WHERE is_active = TRUE AND deletion_mark = FALSE;

-- Scheduled rules lookup
CREATE INDEX idx_sys_auto_rules_trigger_type
    ON sys_automation_rules(trigger_type)
    WHERE is_active = TRUE AND deletion_mark = FALSE;

CREATE INDEX idx_sys_auto_rules_txid
    ON sys_automation_rules(_txid);

CREATE TRIGGER trg_sys_automation_rules_modtime
    BEFORE UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_sys_automation_rules_txid
    BEFORE INSERT OR UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_automation_rules
    BEFORE UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

COMMENT ON TABLE sys_automation_rules IS 'Event-driven automation rules with CEL conditions and pluggable reactions';
COMMENT ON COLUMN sys_automation_rules.event_type IS 'Action: posted, unposted, created, updated, deleted; for scheduled: cron:0 9 * * 1-5';
COMMENT ON COLUMN sys_automation_rules.target_entities IS 'Array of entity keys. NULL = wildcard (matches any entity)';
COMMENT ON COLUMN sys_automation_rules.condition_cel IS 'CEL expression returning boolean. If null/empty → always true';

-- Persisted cursors are initialized at first activation. Existing history is
-- never replayed on the first rollout. Policy can be set per rule: latest/all/skip.
CREATE TABLE sys_automation_schedules (
 rule_id UUID PRIMARY KEY REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
 expression TEXT NOT NULL,
 next_run_at TIMESTAMPTZ NOT NULL,
 catch_up_policy TEXT NOT NULL DEFAULT 'latest' CHECK(catch_up_policy IN ('latest','all','skip')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 _deleted_at TIMESTAMPTZ
);

CREATE TABLE sys_automation_runs (
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
CREATE UNIQUE INDEX uq_automation_runs_processing_rule ON sys_automation_runs(rule_id) WHERE status='processing';
CREATE INDEX idx_automation_runs_due ON sys_automation_runs(next_attempt_at,scheduled_for) WHERE status='pending';
CREATE INDEX idx_automation_runs_expired ON sys_automation_runs(lease_until) WHERE status='processing';
CREATE TRIGGER trg_automation_schedules_txid BEFORE INSERT OR UPDATE ON sys_automation_schedules
 FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_automation_runs_txid BEFORE INSERT OR UPDATE ON sys_automation_runs
 FOR EACH ROW EXECUTE FUNCTION update_txid_column();

-- +goose Down
DROP TABLE IF EXISTS sys_automation_runs;
DROP TABLE IF EXISTS sys_automation_schedules;
DROP TABLE IF EXISTS sys_automation_rules;
