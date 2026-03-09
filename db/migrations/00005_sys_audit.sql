-- +goose Up
-- Description: Audit log for tracking all data changes
-- Partitioned by month for efficient retention management

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Audit action enum
CREATE TYPE audit_action AS ENUM ('create', 'update', 'delete', 'post', 'unpost');

-- Main audit table (partitioned for easy cleanup)
CREATE TABLE sys_audit (
    id UUID NOT NULL DEFAULT gen_random_uuid_v7(),
    entity_type VARCHAR(50) NOT NULL,       -- Table/entity name
    entity_id UUID NOT NULL,                -- Primary key of changed entity
    action audit_action NOT NULL,
    user_id VARCHAR(50),
    user_email VARCHAR(255),
    changes JSONB,                          -- Diff: {"field": {"old": x, "new": y}}
    changes_compressed BYTEA,               -- zstd compressed for large diffs
    compression_algo VARCHAR(10) NOT NULL DEFAULT 'zstd', -- forward compatibility
    metadata JSONB,                         -- Extra context: IP, user agent
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- For fresh installs we keep it simple: default partition catches all rows.
-- Partitions can be managed later by a job/tooling if needed.
CREATE TABLE sys_audit_default PARTITION OF sys_audit DEFAULT;

-- Index for entity history lookup
CREATE INDEX idx_audit_entity ON sys_audit (entity_type, entity_id, created_at DESC);

-- Index for user activity
CREATE INDEX idx_audit_user ON sys_audit (user_id, created_at DESC);

COMMENT ON TABLE sys_audit IS 'Immutable audit log of all data changes';
COMMENT ON COLUMN sys_audit.changes_compressed IS 'zstd compressed JSON for large diffs (>10KB)';
COMMENT ON COLUMN sys_audit.compression_algo IS 'Compression algorithm used: zstd, lz4, none. For forward compatibility.';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS sys_audit;
DROP TYPE IF EXISTS audit_action;
