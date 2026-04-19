-- +goose Up
-- sys_automation_accounts: centralized sender accounts with encrypted credentials.
-- Replaces old sys_service_accounts. Bot Token, SMTP password, API Key — stored once.

CREATE TABLE sys_automation_accounts (
    id                UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    name              VARCHAR(255)  NOT NULL,
    account_type      VARCHAR(30)   NOT NULL,
    -- telegram | email | webhook | rocketchat | slack

    -- Non-secret configuration depending on account_type:
    -- telegram: { "bot_username": "@metapus_bot" }
    -- email:    { "smtp_host": "smtp.gmail.com", "smtp_port": 587,
    --             "from_address": "noreply@c.com", "from_name": "Metapus", "tls": true }
    -- webhook:  { "base_url": "https://api.example.com", "default_headers": {...} }
    config            JSONB         NOT NULL DEFAULT '{}',

    -- Secrets encrypted with AES-256-GCM at application level (Go).
    -- telegram: bot_token
    -- email:    smtp_password
    -- webhook:  api_key / bearer_token
    credentials_enc   BYTEA,

    organization_id   UUID          REFERENCES cat_organizations(id) ON DELETE SET NULL,
    is_active         BOOLEAN       NOT NULL DEFAULT TRUE,
    status            VARCHAR(20)   NOT NULL DEFAULT 'active',
    last_error        TEXT,
    last_success_at   TIMESTAMPTZ,

    deletion_mark     BOOLEAN       NOT NULL DEFAULT FALSE,
    version           INT           NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    _deleted_at       TIMESTAMPTZ,
    _txid             BIGINT        DEFAULT txid_current()
);



CREATE INDEX idx_sys_auto_accounts_type
    ON sys_automation_accounts(account_type, status);

CREATE INDEX idx_sys_auto_accounts_txid
    ON sys_automation_accounts(_txid);

CREATE TRIGGER trg_sys_automation_accounts_modtime
    BEFORE UPDATE ON sys_automation_accounts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_sys_automation_accounts_txid
    BEFORE INSERT OR UPDATE ON sys_automation_accounts
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_automation_accounts
    BEFORE UPDATE ON sys_automation_accounts
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

COMMENT ON TABLE sys_automation_accounts IS
    'Centralized sender accounts with encrypted credentials (Bot Token, SMTP password, API Key)';
COMMENT ON COLUMN sys_automation_accounts.credentials_enc IS
    'AES-256-GCM encrypted in Go app layer. Never exposed in API responses.';

-- +goose Down
DROP TABLE IF EXISTS sys_automation_accounts;
