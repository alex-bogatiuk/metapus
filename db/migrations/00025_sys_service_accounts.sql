-- +goose Up
-- sys_service_accounts: stores external integration configurations and encrypted credentials.

CREATE TABLE sys_service_accounts (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    name            VARCHAR(100)    NOT NULL,
    account_type    VARCHAR(50)     NOT NULL, -- e.g., 'telegram', 'email', 'webhook'
    config          JSONB           NOT NULL DEFAULT '{}',
    credentials_enc BYTEA,
    organization_id UUID            REFERENCES cat_organizations(id) ON DELETE SET NULL,
    status          VARCHAR(50)     NOT NULL DEFAULT 'active',
    is_default      BOOLEAN         NOT NULL DEFAULT FALSE,
    last_error      TEXT,
    last_success_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Ensure only one default account per type, per organization.
-- For global accounts (organization_id is null), we use a zero-UUID for the unique constraint.
CREATE UNIQUE INDEX idx_sys_service_accounts_default_org
ON sys_service_accounts (account_type, COALESCE(organization_id, '00000000-0000-0000-0000-000000000000')) 
WHERE is_default = TRUE;

-- Index for fast lookup by type
CREATE INDEX idx_sys_service_accounts_type ON sys_service_accounts (account_type, status);

CREATE TRIGGER trg_sys_service_accounts_updated_at
    BEFORE UPDATE ON sys_service_accounts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE sys_service_accounts IS 'External service accounts and integrations configurations';
COMMENT ON COLUMN sys_service_accounts.credentials_enc IS 'Credentials encrypted with pgcrypto (pgp_sym_encrypt)';

-- +goose Down
DROP TABLE IF EXISTS sys_service_accounts;
