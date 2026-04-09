-- Meta-database schema for multi-tenant management
CREATE TABLE IF NOT EXISTS tenants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            VARCHAR(63) NOT NULL,
    display_name    VARCHAR(255) NOT NULL,
    db_name         VARCHAR(63) NOT NULL UNIQUE,
    db_host         VARCHAR(255) NOT NULL DEFAULT 'localhost',
    db_port         INT NOT NULL DEFAULT 5432,
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    plan            VARCHAR(50) NOT NULL DEFAULT 'standard',
    schema_version  INT NOT NULL DEFAULT 0,
    version_group   VARCHAR(20) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settings        JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tenants_slug_lower ON tenants (lower(slug));
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
CREATE INDEX IF NOT EXISTS idx_tenants_status_slug ON tenants(status, slug) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_tenants_version_group ON tenants(version_group, status) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS tenant_migrations (
    id          SERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    version     INT NOT NULL,
    name        VARCHAR(255) NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    checksum    VARCHAR(64),
    duration_ms INT,
    UNIQUE(tenant_id, version)
);
CREATE INDEX IF NOT EXISTS idx_tenant_migrations_tenant ON tenant_migrations(tenant_id);

CREATE TABLE IF NOT EXISTS tenant_audit (
    id          SERIAL PRIMARY KEY,
    tenant_id   UUID REFERENCES tenants(id) ON DELETE SET NULL,
    action      VARCHAR(50) NOT NULL,
    actor       VARCHAR(255),
    details     JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tenant_audit_tenant ON tenant_audit(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tenant_audit_created ON tenant_audit(created_at DESC);

CREATE OR REPLACE FUNCTION update_tenant_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_tenants_updated_at ON tenants;
CREATE TRIGGER trigger_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_tenant_timestamp();
