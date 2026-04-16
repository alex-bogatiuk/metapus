-- +goose Up
-- sys_automation_rules: stores conditions and templates for trigger actions.

CREATE TABLE sys_automation_rules (
    id                 UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    name               VARCHAR(100)  NOT NULL,
    organization_id    UUID          REFERENCES cat_organizations(id) ON DELETE CASCADE,
    event_type         VARCHAR(100)  NOT NULL, 
    condition_cel      TEXT,                 
    action_type        VARCHAR(50)   NOT NULL,  
    action_template    TEXT          NOT NULL,        
    service_account_id UUID          REFERENCES sys_service_accounts(id) ON DELETE RESTRICT,
    is_active          BOOLEAN       NOT NULL DEFAULT TRUE,

    version            INT           NOT NULL DEFAULT 1,
    deletion_mark      BOOLEAN       NOT NULL DEFAULT FALSE,

    created_at         TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    _deleted_at        TIMESTAMPTZ,
    _txid              BIGINT        DEFAULT txid_current()
);

-- Index for quickly loading active rules by event type
CREATE INDEX idx_sys_automation_rules_event_type 
ON sys_automation_rules (event_type) 
WHERE is_active = TRUE;

CREATE INDEX idx_sys_automation_rules_txid ON sys_automation_rules(_txid);

CREATE TRIGGER trg_sys_automation_rules_modtime
    BEFORE UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION update_modified_column();

CREATE TRIGGER set_sys_automation_rules_txid
    BEFORE INSERT OR UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_automation_rules
    BEFORE UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

COMMENT ON TABLE sys_automation_rules IS 'Automation rules to be evaluated by the CEL engine on system events';
COMMENT ON COLUMN sys_automation_rules.event_type IS 'e.g., document.goods_receipt.posted';
COMMENT ON COLUMN sys_automation_rules.condition_cel IS 'CEL expression returning boolean. If null/empty, assumed true';
COMMENT ON COLUMN sys_automation_rules.action_template IS 'go text/template for the payload or message content';

-- +goose Down
DROP TABLE IF EXISTS sys_automation_rules;
