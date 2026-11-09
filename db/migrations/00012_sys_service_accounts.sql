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


-- Tenant-local integration accounts and ordered pools. No plaintext credentials.
CREATE TABLE cat_integration_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code VARCHAR(50) NOT NULL, name VARCHAR(255) NOT NULL,
    parent_id UUID, is_folder BOOLEAN NOT NULL DEFAULT FALSE CHECK (NOT is_folder),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE, version INT NOT NULL DEFAULT 1,
    attributes JSONB NOT NULL DEFAULT '{}', _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider TEXT NOT NULL CHECK (provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api')),
    external_account_ref VARCHAR(255) NOT NULL DEFAULT '', is_active BOOLEAN NOT NULL DEFAULT TRUE,
    reset_day INT NOT NULL DEFAULT 1 CHECK (reset_day BETWEEN 1 AND 31),
    reset_timezone TEXT NOT NULL DEFAULT 'UTC', credentials_enc BYTEA,
    credentials_version INT NOT NULL DEFAULT 0 CHECK (credentials_version >= 0),
    credentials_updated_at TIMESTAMPTZ,
    credentials_expires_at TIMESTAMPTZ, manual_period_started_at TIMESTAMPTZ,
    manual_period_version BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT cat_integration_accounts_manual_period_check CHECK (
      (provider='drpc' AND credentials_expires_at IS NOT NULL AND manual_period_started_at IS NOT NULL
       AND manual_period_version>0 AND credentials_expires_at>manual_period_started_at)
      OR (provider<>'drpc' AND credentials_expires_at IS NULL AND manual_period_started_at IS NULL AND manual_period_version=0)),
    CHECK (parent_id IS NULL)
);
CREATE UNIQUE INDEX uq_integration_accounts_external ON cat_integration_accounts(provider,external_account_ref)
 WHERE external_account_ref <> '' AND NOT deletion_mark;
CREATE TABLE cat_integration_pools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code VARCHAR(50) NOT NULL, name VARCHAR(255) NOT NULL,
    parent_id UUID, is_folder BOOLEAN NOT NULL DEFAULT FALSE CHECK (NOT is_folder),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE, version INT NOT NULL DEFAULT 1,
    attributes JSONB NOT NULL DEFAULT '{}', _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider TEXT NOT NULL CHECK (provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api')), is_active BOOLEAN NOT NULL DEFAULT TRUE,
    CHECK (parent_id IS NULL)
);
CREATE TABLE sys_integration_pool_accounts (
    pool_id UUID NOT NULL REFERENCES cat_integration_pools(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES cat_integration_accounts(id) ON DELETE RESTRICT,
    position INT NOT NULL CHECK (position >= 0), PRIMARY KEY(pool_id,account_id), UNIQUE(pool_id,position)
);
CREATE INDEX idx_integration_pool_account ON sys_integration_pool_accounts(account_id);
CREATE TABLE sys_integration_account_state (
    account_id UUID PRIMARY KEY REFERENCES cat_integration_accounts(id) ON DELETE CASCADE,
    period_start TIMESTAMPTZ NOT NULL, period_end TIMESTAMPTZ NOT NULL,
    exhausted_until TIMESTAMPTZ, throttled_until TIMESTAMPTZ, probe_lease_until TIMESTAMPTZ,
    invalid_credentials_version INT NOT NULL DEFAULT -1, invalid_slots TEXT[] NOT NULL DEFAULT '{}',
    manual_period_version BIGINT NOT NULL DEFAULT 0 CHECK (manual_period_version>=0),
    last_error TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), version BIGINT NOT NULL DEFAULT 1
);
CREATE TABLE sys_integration_pool_state (
    pool_id UUID PRIMARY KEY REFERENCES cat_integration_pools(id) ON DELETE CASCADE,
    account_id UUID REFERENCES cat_integration_accounts(id) ON DELETE RESTRICT,
    position INT NOT NULL DEFAULT -1, selection_version BIGINT NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX uq_cat_integration_accounts_code ON cat_integration_accounts(code) WHERE NOT deletion_mark;
CREATE INDEX idx_cat_integration_accounts_name_id ON cat_integration_accounts(name,id);
CREATE INDEX idx_cat_integration_accounts_txid ON cat_integration_accounts(_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_cat_integration_accounts_txid BEFORE INSERT OR UPDATE ON cat_integration_accounts FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_cat_integration_accounts_modtime BEFORE UPDATE ON cat_integration_accounts FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_cat_integration_accounts_delete BEFORE UPDATE OF deletion_mark ON cat_integration_accounts FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE UNIQUE INDEX uq_cat_integration_pools_code ON cat_integration_pools(code) WHERE NOT deletion_mark;
CREATE INDEX idx_cat_integration_pools_name_id ON cat_integration_pools(name,id);
CREATE INDEX idx_cat_integration_pools_txid ON cat_integration_pools(_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_cat_integration_pools_txid BEFORE INSERT OR UPDATE ON cat_integration_pools FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_cat_integration_pools_modtime BEFORE UPDATE ON cat_integration_pools FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_cat_integration_pools_delete BEFORE UPDATE OF deletion_mark ON cat_integration_pools FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
INSERT INTO auth_permissions(code,name,description,resource,action)
SELECT 'catalog:'||resource||':'||action, label||': '||action, label,resource,action
FROM (VALUES ('integration_account','Аккаунты интеграций'),('integration_pool','Пулы аккаунтов')) AS r(resource,label)
CROSS JOIN (VALUES ('read'),('create'),('update'),('delete')) AS a(action);
INSERT INTO auth_role_permissions(role_id,permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001',id FROM auth_permissions WHERE resource IN ('integration_account','integration_pool');

-- +goose Down
DELETE FROM auth_role_permissions WHERE permission_id IN (SELECT id FROM auth_permissions WHERE resource IN ('integration_account','integration_pool'));
DELETE FROM auth_permissions WHERE resource IN ('integration_account','integration_pool');
DROP TABLE sys_integration_pool_state;
DROP TABLE sys_integration_account_state;
DROP TABLE sys_integration_pool_accounts;
DROP TABLE cat_integration_pools;
DROP TABLE cat_integration_accounts;

DROP TABLE IF EXISTS sys_automation_accounts;
