-- +goose Up
-- sys_automation_history: append-only log of rule evaluations and deliveries.
-- Denormalized for fast UI display without JOINs.

CREATE TABLE sys_automation_history (
    id                UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    rule_id           UUID          NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
    rule_name         VARCHAR(255)  NOT NULL,

    event_type        VARCHAR(100)  NOT NULL,
    aggregate_id      UUID,
    aggregate_name    VARCHAR(255),

    status            VARCHAR(20)   NOT NULL,
    -- success | error | condition_false | skipped | pending

    channel_id        UUID,
    channel_name      VARCHAR(255),
    account_name      VARCHAR(255),

    rendered_payload  TEXT,
    error_text        TEXT,
    duration_ms       INT,

    created_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp()
);

-- Main UI query: recent history with filters
CREATE INDEX idx_sys_auto_hist_created
    ON sys_automation_history(created_at DESC);

CREATE INDEX idx_sys_auto_hist_rule
    ON sys_automation_history(rule_id);

CREATE INDEX idx_sys_auto_hist_status
    ON sys_automation_history(status);

-- Fast "recent errors" query for dashboard/alerts
CREATE INDEX idx_sys_auto_hist_errors
    ON sys_automation_history(created_at DESC)
    WHERE status = 'error';

COMMENT ON TABLE sys_automation_history IS
    'Append-only execution log for automation rules. Denormalized for fast UI queries.';

-- +goose Down
DROP TABLE IF EXISTS sys_automation_history;
