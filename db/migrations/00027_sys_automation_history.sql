-- +goose Up
-- sys_automation_history: logs each rule execution for observability.

CREATE TABLE IF NOT EXISTS sys_automation_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    rule_id UUID NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    aggregate_id UUID NOT NULL,
    success BOOLEAN NOT NULL,
    error_message TEXT,
    request_payload TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp()
);

CREATE INDEX IF NOT EXISTS idx_sys_automation_history_rule ON sys_automation_history(rule_id);
CREATE INDEX IF NOT EXISTS idx_sys_automation_history_created_at ON sys_automation_history(created_at);

-- +goose Down
DROP TABLE IF EXISTS sys_automation_history;
