-- +goose Up
-- Meta-database schema for multi-tenant management
-- This database stores the registry of all tenants and their database connections

-- Tenants table - registry of all tenant databases
CREATE TABLE IF NOT EXISTS tenants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            VARCHAR(63) NOT NULL,             -- URL-safe identifier (acme, globex)
    display_name    VARCHAR(255) NOT NULL,            -- Human-readable name
    db_name         VARCHAR(63) NOT NULL UNIQUE,      -- Database name (mt_acme, mt_globex)
    db_host         VARCHAR(255) NOT NULL DEFAULT 'localhost',
    db_port         INT NOT NULL DEFAULT 5432,
    status          VARCHAR(20) NOT NULL DEFAULT 'active',  -- active, suspended, deleted
    plan            VARCHAR(50) NOT NULL DEFAULT 'standard', -- standard, premium, enterprise
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settings        JSONB NOT NULL DEFAULT '{}'       -- Additional tenant settings
);

-- Indexes for common queries
CREATE INDEX idx_tenants_slug ON tenants(slug);
-- Enforce case-insensitive uniqueness: prevent duplicates like "Acme" vs "acme"
CREATE UNIQUE INDEX uq_tenants_slug_lower ON tenants (lower(slug));
CREATE INDEX idx_tenants_status ON tenants(status);
CREATE INDEX idx_tenants_status_slug ON tenants(status, slug) WHERE status = 'active';

-- Tenant migrations tracking
-- Tracks which migrations have been applied to each tenant database
CREATE TABLE IF NOT EXISTS tenant_migrations (
    id          SERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    version     INT NOT NULL,
    name        VARCHAR(255) NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    checksum    VARCHAR(64),  -- SHA256 of migration file for integrity check
    duration_ms INT,          -- How long the migration took
    UNIQUE(tenant_id, version)
);

CREATE INDEX idx_tenant_migrations_tenant ON tenant_migrations(tenant_id);

-- Audit log for tenant operations
CREATE TABLE IF NOT EXISTS tenant_audit (
    id          SERIAL PRIMARY KEY,
    tenant_id   UUID REFERENCES tenants(id) ON DELETE SET NULL,
    action      VARCHAR(50) NOT NULL,  -- created, suspended, activated, deleted, migrated
    actor       VARCHAR(255),          -- Who performed the action
    details     JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tenant_audit_tenant ON tenant_audit(tenant_id);
CREATE INDEX idx_tenant_audit_created ON tenant_audit(created_at DESC);

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

CREATE TRIGGER trigger_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_tenant_timestamp();

-- +goose Down
DROP TRIGGER IF EXISTS trigger_tenants_updated_at ON tenants;
DROP FUNCTION IF EXISTS update_tenant_timestamp();
DROP TABLE IF EXISTS tenant_audit;
DROP TABLE IF EXISTS tenant_migrations;
DROP TABLE IF EXISTS tenants;
