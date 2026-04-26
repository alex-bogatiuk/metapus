-- +goose Up
-- sys_automation_channels: delivery destinations that reference a centralized Account.
-- One Account (Bot Token) → many Channels (different Chat IDs).

CREATE TABLE sys_automation_channels (
    id                UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    name              VARCHAR(255)  NOT NULL,

    account_id        UUID          NOT NULL REFERENCES sys_automation_accounts(id) ON DELETE RESTRICT,
    -- RESTRICT: cannot delete account while channels exist

    -- Destination config depending on account_type:
    -- telegram: { "chat_id": "-1001234567890", "parse_mode": "Markdown", "thread_id": 123 }
    -- email:    { "to": ["user@company.com"], "cc": [], "subject_prefix": "[ERP]" }
    -- webhook:  { "url": "/hook/goods-receipt", "method": "POST", "headers": {...} }
    destination       JSONB         NOT NULL DEFAULT '{}',

    is_active         BOOLEAN       NOT NULL DEFAULT TRUE,

    deletion_mark     BOOLEAN       NOT NULL DEFAULT FALSE,
    version           INT           NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    _deleted_at       TIMESTAMPTZ,
    _txid             BIGINT        DEFAULT txid_current()
);

CREATE INDEX idx_sys_auto_channels_account
    ON sys_automation_channels(account_id);

CREATE INDEX idx_sys_auto_channels_txid
    ON sys_automation_channels(_txid);

CREATE TRIGGER trg_sys_automation_channels_modtime
    BEFORE UPDATE ON sys_automation_channels
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_sys_automation_channels_txid
    BEFORE INSERT OR UPDATE ON sys_automation_channels
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_automation_channels
    BEFORE UPDATE ON sys_automation_channels
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

COMMENT ON TABLE sys_automation_channels IS
    'Delivery destinations (Telegram chat, email address, webhook URL). References Account for credentials.';
COMMENT ON COLUMN sys_automation_channels.destination IS
    'Channel-specific config: chat_id for Telegram, to[] for Email, url for Webhook';

-- +goose Down
DROP TABLE IF EXISTS sys_automation_channels;
