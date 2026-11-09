-- +goose Up
-- Meta-database schema for multi-tenant management
-- This database stores the registry of all tenants and their database connections

CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION gen_random_uuid_v7()
RETURNS uuid AS $func$
DECLARE
    v_time bigint;
    v_rand bytea;
    v_uuid uuid;
BEGIN
    v_time := (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::bigint;
    v_rand := gen_random_bytes(10);
    v_uuid := (
        lpad(to_hex(v_time >> 16), 8, '0') ||
        lpad(to_hex(v_time & 65535), 4, '0') ||
        '7' || substr(encode(v_rand, 'hex'), 1, 3) ||
        to_hex((get_byte(v_rand, 2) & 63) | 128) ||
        substr(encode(v_rand, 'hex'), 5, 2) ||
        substr(encode(v_rand, 'hex'), 7, 12)
    )::uuid;
    RETURN v_uuid;
END;
$func$ LANGUAGE plpgsql VOLATILE;
-- +goose StatementEnd

-- Tenants table - registry of all tenant databases
CREATE TABLE IF NOT EXISTS tenants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    slug            VARCHAR(63) NOT NULL,             -- URL-safe identifier (acme, globex)
    display_name    VARCHAR(255) NOT NULL,            -- Human-readable name
    db_name         VARCHAR(63) NOT NULL UNIQUE,      -- Database name (mt_acme, mt_globex)
    db_host         VARCHAR(255) NOT NULL DEFAULT 'localhost',
    db_port         INT NOT NULL DEFAULT 5432,
    status          VARCHAR(20) NOT NULL DEFAULT 'provisioning',
    plan            VARCHAR(50) NOT NULL DEFAULT 'standard', -- standard, premium, enterprise
    schema_version  INT NOT NULL DEFAULT 0,           -- Highest applied goose migration number
    provisioning_stage VARCHAR(32) NOT NULL DEFAULT '',
    provisioning_error TEXT NOT NULL DEFAULT '',
    provisioned_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settings        JSONB NOT NULL DEFAULT '{}'       -- Additional tenant settings
);

-- The first version of init-meta created this table outside goose. Keep the
-- initial migration repair-safe so existing installations can adopt goose.
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS provisioning_stage VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS provisioning_error TEXT NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS provisioned_at TIMESTAMPTZ;

UPDATE tenants
SET provisioned_at = COALESCE(provisioned_at, created_at)
WHERE status IN ('active', 'suspended')
  AND provisioned_at IS NULL;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_tenants_status'
          AND conrelid = 'tenants'::regclass
    ) THEN
        ALTER TABLE tenants
            ADD CONSTRAINT chk_tenants_status
            CHECK (status IN (
                'provisioning', 'provision_failed', 'active', 'suspended', 'deleted'
            ));
    END IF;
END
$$;
-- +goose StatementEnd

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
-- Enforce case-insensitive uniqueness: prevent duplicates like "Acme" vs "acme"
CREATE UNIQUE INDEX IF NOT EXISTS uq_tenants_slug_lower ON tenants (lower(slug));
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
CREATE INDEX IF NOT EXISTS idx_tenants_status_slug ON tenants(status, slug) WHERE status = 'active';

-- Merchant API key hot-path index.
-- Public merchant API auth resolves tenant from the key hash before opening a tenant DB pool.
CREATE TABLE IF NOT EXISTS merchant_api_key_index (
    key_hash      CHAR(64) PRIMARY KEY,
    key_prefix    VARCHAR(16) NOT NULL,
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    api_key_id    UUID NOT NULL,
    merchant_id   UUID NOT NULL,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_merchant_api_key_index_api_key
    ON merchant_api_key_index (tenant_id, api_key_id);
CREATE INDEX IF NOT EXISTS idx_merchant_api_key_index_active
    ON merchant_api_key_index (key_hash)
    WHERE is_active = TRUE;

-- Public checkout reference hot-path index.
-- Customer-facing URLs carry only opaque public refs. The meta DB stores only
-- SHA-256(ref), resolves the tenant, and never stores bearer checkout refs raw.
CREATE TABLE IF NOT EXISTS public_checkout_refs (
    ref_hash      CHAR(64) PRIMARY KEY,
    ref_prefix    VARCHAR(8) NOT NULL,
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    merchant_id   UUID NOT NULL,
    resource_type VARCHAR(32) NOT NULL,
    resource_id   UUID NOT NULL,
    status        VARCHAR(16) NOT NULL DEFAULT 'reserved',
    expires_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_public_checkout_refs_resource UNIQUE (tenant_id, resource_type, resource_id),
    CONSTRAINT chk_public_checkout_refs_prefix CHECK (ref_prefix IN ('ci', 'pl')),
    CONSTRAINT chk_public_checkout_refs_type CHECK (resource_type IN ('crypto_invoice', 'payment_link')),
    CONSTRAINT chk_public_checkout_refs_status CHECK (status IN ('reserved', 'active', 'disabled'))
);

CREATE INDEX IF NOT EXISTS idx_public_checkout_refs_active
    ON public_checkout_refs (ref_hash)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_public_checkout_refs_resource
    ON public_checkout_refs (tenant_id, resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_public_checkout_refs_reserved_expiry
    ON public_checkout_refs (expires_at)
    WHERE status = 'reserved' AND expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS checkout_domains (
    host        VARCHAR(255) PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    merchant_id UUID,
    status      VARCHAR(16) NOT NULL DEFAULT 'active',
    verified_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_checkout_domains_host_lower CHECK (host = lower(host)),
    CONSTRAINT chk_checkout_domains_status CHECK (status IN ('active', 'disabled', 'pending_verification'))
);

CREATE INDEX IF NOT EXISTS idx_checkout_domains_tenant
    ON checkout_domains (tenant_id)
    WHERE status = 'active';

-- Audit log for tenant operations
CREATE TABLE IF NOT EXISTS tenant_audit (
    id          SERIAL PRIMARY KEY,
    tenant_id   UUID REFERENCES tenants(id) ON DELETE SET NULL,
    action      VARCHAR(50) NOT NULL,  -- created, suspended, activated, deleted, migrated
    actor       VARCHAR(255),          -- Who performed the action
    details     JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tenant_audit_tenant ON tenant_audit(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tenant_audit_created ON tenant_audit(created_at DESC);

-- Trigger to update updated_at on tenants
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_tenant_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trigger_tenants_updated_at ON tenants;
CREATE TRIGGER trigger_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_tenant_timestamp();

DROP TRIGGER IF EXISTS trigger_merchant_api_key_index_updated_at ON merchant_api_key_index;
CREATE TRIGGER trigger_merchant_api_key_index_updated_at
    BEFORE UPDATE ON merchant_api_key_index
    FOR EACH ROW
    EXECUTE FUNCTION update_tenant_timestamp();

DROP TRIGGER IF EXISTS trigger_public_checkout_refs_updated_at ON public_checkout_refs;
CREATE TRIGGER trigger_public_checkout_refs_updated_at
    BEFORE UPDATE ON public_checkout_refs
    FOR EACH ROW
    EXECUTE FUNCTION update_tenant_timestamp();

DROP TRIGGER IF EXISTS trigger_checkout_domains_updated_at ON checkout_domains;
CREATE TRIGGER trigger_checkout_domains_updated_at
    BEFORE UPDATE ON checkout_domains
    FOR EACH ROW
    EXECUTE FUNCTION update_tenant_timestamp();

-- +goose Down
DROP TRIGGER IF EXISTS trigger_checkout_domains_updated_at ON checkout_domains;
DROP TRIGGER IF EXISTS trigger_public_checkout_refs_updated_at ON public_checkout_refs;
DROP TRIGGER IF EXISTS trigger_merchant_api_key_index_updated_at ON merchant_api_key_index;
DROP TRIGGER IF EXISTS trigger_tenants_updated_at ON tenants;
DROP FUNCTION IF EXISTS update_tenant_timestamp();
DROP TABLE IF EXISTS checkout_domains;
DROP TABLE IF EXISTS public_checkout_refs;
DROP TABLE IF EXISTS merchant_api_key_index;
DROP TABLE IF EXISTS tenant_audit;
DROP TABLE IF EXISTS tenants;

DROP FUNCTION IF EXISTS gen_random_uuid_v7();
