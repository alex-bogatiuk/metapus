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
    -- Array of entity keys: ['goods_receipt','goods_issue']. NULL = any entity (wildcard).

    -- CONDITION
    condition_cel      TEXT,

    -- REACTION
    reaction_type      VARCHAR(30)   NOT NULL DEFAULT 'notify',
    -- notify | webhook_call | chain | create_record
    message_format     VARCHAR(20)   NOT NULL DEFAULT 'text',
    -- text | markdown | html
    action_template    TEXT          NOT NULL DEFAULT '',

    -- CHAIN (only for reaction_type = 'chain')
    chain_rule_ids     UUID[],

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

-- GIN index for array matching: 'goods_receipt' = ANY(target_entities)
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
COMMENT ON COLUMN sys_automation_rules.chain_rule_ids IS 'Array of rule IDs to trigger (only for reaction_type=chain)';

-- +goose Down
DROP TABLE IF EXISTS sys_automation_rules;
