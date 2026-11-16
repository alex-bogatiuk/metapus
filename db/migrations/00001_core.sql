-- Core platform objects: extensions, shared helper functions, outbox, idempotency,
-- audit, sequences and the event log.
-- Consolidated schema: each object is declared once in its final form.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.
SET LOCAL check_function_bodies = off;


-- ---- Extensions ----
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;
CREATE EXTENSION IF NOT EXISTS btree_gist;


-- ---- Types ----
-- sys_outbox (transactional outbox pattern)
CREATE TYPE outbox_status AS ENUM ('pending', 'processing', 'published', 'failed');

CREATE TYPE idempotency_status AS ENUM ('pending', 'success', 'failed');

-- sys_audit (partitioned)
CREATE TYPE audit_action AS ENUM ('create', 'update', 'delete', 'post', 'unpost');


-- ---- Functions ----
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

-- Auto-update updated_at timestamp on any UPDATE
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $func$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$func$ LANGUAGE plpgsql;

-- CDC: update _txid on any change
CREATE OR REPLACE FUNCTION update_txid_column()
RETURNS TRIGGER AS $func$
BEGIN
    NEW._txid = txid_current();
    RETURN NEW;
END;
$func$ LANGUAGE plpgsql;

-- CDC: set _deleted_at timestamp when deletion_mark toggled
CREATE OR REPLACE FUNCTION soft_delete_with_timestamp()
RETURNS TRIGGER AS $func$
BEGIN
    IF NEW.deletion_mark = TRUE AND OLD.deletion_mark = FALSE THEN
        NEW._deleted_at = NOW();
    ELSIF NEW.deletion_mark = FALSE AND OLD.deletion_mark = TRUE THEN
        NEW._deleted_at = NULL;
    END IF;
    RETURN NEW;
END;
$func$ LANGUAGE plpgsql;

-- Safe text → UUID conversion
CREATE OR REPLACE FUNCTION safe_uuid(p_text TEXT)
RETURNS UUID AS $func$
BEGIN
    RETURN p_text::uuid;
EXCEPTION WHEN invalid_text_representation THEN
    RETURN NULL;
END;
$func$ LANGUAGE plpgsql IMMUTABLE;

-- UUID format validation
CREATE OR REPLACE FUNCTION is_valid_uuid(p_text TEXT)
RETURNS BOOLEAN AS $func$
BEGIN
    RETURN p_text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
END;
$func$ LANGUAGE plpgsql IMMUTABLE;

-- Scope, operation, key and request fingerprint are durable command identity.
-- They must survive result archival as a tombstone and can never be rewritten.
CREATE OR REPLACE FUNCTION fn_sys_idempotency_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'idempotency commands cannot be deleted';
    END IF;

    IF NEW.scope_type <> OLD.scope_type
       OR NEW.scope_id <> OLD.scope_id
       OR NEW.operation <> OLD.operation
       OR NEW.idempotency_key <> OLD.idempotency_key
       OR NEW.request_fingerprint <> OLD.request_fingerprint THEN
        RAISE EXCEPTION 'idempotency command identity is immutable';
    END IF;

    IF OLD.status = 'pending'
       AND NEW.status IN ('success','failed')
       AND NEW.created_at = OLD.created_at
       AND NEW.archived_at IS NULL THEN
        RETURN NEW;
    END IF;

    -- Archival may discard the serialized response body, but it preserves a
    -- durable command/resource tombstone and every semantic result field.
    IF OLD.status IN ('success','failed')
       AND OLD.archived_at IS NULL
       AND NEW.archived_at IS NOT NULL
       AND NEW.application_result = '{}'::jsonb
       AND NEW.status = OLD.status
       AND NEW.outcome = OLD.outcome
       AND NEW.resource_type = OLD.resource_type
       AND NEW.resource_id = OLD.resource_id
       AND NEW.resource_version = OLD.resource_version
       AND NEW.created_at = OLD.created_at
       AND NEW.completed_at = OLD.completed_at THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'idempotency command result is immutable';
END;
$$ LANGUAGE plpgsql;


-- ---- Tables ----
CREATE TABLE sys_sequences (
    key         VARCHAR(100) NOT NULL PRIMARY KEY,
    current_val BIGINT       NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE sys_sequences IS 'Auto-numbering sequences for documents (INV-2024-00001)';
COMMENT ON COLUMN sys_sequences.key IS 'Sequence key: {prefix}_{period}, e.g., INVOICE_2024';

CREATE TABLE sys_outbox (
    id             UUID          NOT NULL,
    aggregate_type VARCHAR(50)   NOT NULL,
    aggregate_id   UUID          NOT NULL,
    event_type     VARCHAR(50)   NOT NULL,
    payload        JSONB         NOT NULL,
    status         outbox_status NOT NULL DEFAULT 'pending',
    retry_count    INT           NOT NULL DEFAULT 0,
    last_error     TEXT,
    next_retry_at  TIMESTAMPTZ,
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    published_at   TIMESTAMPTZ,
    claim_token    UUID,
    claimed_at     TIMESTAMPTZ,
    lease_until    TIMESTAMPTZ,
    traceparent    TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    causation_id   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at
);
CREATE INDEX idx_outbox_pending ON sys_outbox (created_at) WHERE status = 'pending';
CREATE INDEX idx_outbox_retry   ON sys_outbox (next_retry_at) WHERE status = 'pending' AND next_retry_at IS NOT NULL;
CREATE INDEX idx_outbox_stuck   ON sys_outbox (lease_until) WHERE status = 'processing';

CREATE TABLE sys_outbox_default PARTITION OF sys_outbox DEFAULT;

CREATE TABLE sys_outbox_dlq (
    id             UUID        PRIMARY KEY,
    aggregate_type VARCHAR(50) NOT NULL,
    aggregate_id   UUID        NOT NULL,
    event_type     VARCHAR(50) NOT NULL,
    payload        JSONB       NOT NULL,
    retry_count    INT         NOT NULL,
    last_error     TEXT,
    created_at     TIMESTAMPTZ NOT NULL,
    failed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    failure_reason TEXT,
    traceparent    TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    causation_id   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE sys_idempotency (
    scope_type           TEXT               NOT NULL
                         CHECK (scope_type ~ '^[a-z][a-z_]{0,31}$'),
    scope_id             TEXT               NOT NULL CHECK (BTRIM(scope_id) <> ''),
    operation            VARCHAR(200)       NOT NULL CHECK (BTRIM(operation) <> ''),
    idempotency_key      VARCHAR(200)       NOT NULL CHECK (BTRIM(idempotency_key) <> ''),
    request_fingerprint  CHAR(64)           NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    status               idempotency_status NOT NULL DEFAULT 'pending',
    outcome              TEXT               NOT NULL DEFAULT '',
    resource_type        TEXT               NOT NULL DEFAULT '',
    resource_id          TEXT               NOT NULL DEFAULT '',
    resource_version     BIGINT             NOT NULL DEFAULT 0 CHECK (resource_version >= 0),
    application_result   JSONB              NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ        NOT NULL DEFAULT NOW(),
    completed_at         TIMESTAMPTZ,
    archived_at          TIMESTAMPTZ,
    PRIMARY KEY (scope_type, scope_id, operation, idempotency_key),
    CONSTRAINT chk_sys_idempotency_completion CHECK (
        (status = 'pending' AND completed_at IS NULL)
        OR (status IN ('success','failed') AND completed_at IS NOT NULL AND outcome <> '')
    )
);
CREATE INDEX idx_idempotency_resource
    ON sys_idempotency (resource_type, resource_id)
    WHERE resource_type <> '' AND resource_id <> '';
COMMENT ON TABLE sys_idempotency IS 'Durable application command results. Financial command identities are never deleted.';

CREATE TABLE sys_audit (
    id                 UUID         NOT NULL DEFAULT gen_random_uuid_v7(),
    entity_type        VARCHAR(50)  NOT NULL,
    entity_id          UUID         NOT NULL,
    action             audit_action NOT NULL,
    user_id            VARCHAR(50),
    user_email         VARCHAR(255),
    changes            JSONB,
    changes_compressed BYTEA,
    compression_algo   VARCHAR(10)  NOT NULL DEFAULT 'zstd',
    metadata           JSONB,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at
);
CREATE INDEX idx_audit_entity ON sys_audit (entity_type, entity_id, created_at DESC);
CREATE INDEX idx_audit_user   ON sys_audit (user_id, created_at DESC);
COMMENT ON TABLE sys_audit IS 'Immutable audit log of all data changes';

CREATE TABLE sys_audit_default PARTITION OF sys_audit DEFAULT;

-- sys_event_log (partitioned)
CREATE TABLE sys_event_log (
    id           UUID         NOT NULL DEFAULT gen_random_uuid_v7(),
    category     VARCHAR(30)  NOT NULL,
    severity     VARCHAR(20)  NOT NULL DEFAULT 'info',
    event_type   VARCHAR(50)  NOT NULL,
    source       VARCHAR(30)  NOT NULL DEFAULT 'api',
    session_id   VARCHAR(50),
    user_id      VARCHAR(50),
    client_ip    INET,
    entity_type  VARCHAR(50),
    entity_id    UUID,
    entity_number VARCHAR(50),
    message      TEXT         NOT NULL,
    details      JSONB,
    trace_id     VARCHAR(36),
    request_id   VARCHAR(36),
    duration_ms  INT,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at
);
CREATE INDEX idx_event_log_category ON sys_event_log (category, created_at DESC);
CREATE INDEX idx_event_log_user     ON sys_event_log (user_id, created_at DESC);
CREATE INDEX idx_event_log_entity   ON sys_event_log (entity_type, entity_id, created_at DESC);
CREATE INDEX idx_event_log_severity ON sys_event_log (severity, created_at DESC) WHERE severity IN ('error', 'critical');
CREATE INDEX idx_event_log_trace    ON sys_event_log (trace_id, created_at ASC) WHERE trace_id IS NOT NULL;
CREATE INDEX idx_event_log_message_trgm ON sys_event_log USING gin (message gin_trgm_ops);
CREATE INDEX idx_event_log_entity_number ON sys_event_log (entity_number, created_at DESC) WHERE entity_number IS NOT NULL;
COMMENT ON TABLE sys_event_log IS 'System event log — unified journal (analogue of 1C ЖурналРегистрации)';

CREATE TABLE sys_event_log_default PARTITION OF sys_event_log DEFAULT;


-- ---- Triggers ----
CREATE TRIGGER trg_sys_sequences_updated_at
    BEFORE UPDATE ON sys_sequences
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_sys_idempotency_guard
    BEFORE UPDATE OR DELETE ON sys_idempotency
    FOR EACH ROW EXECUTE FUNCTION fn_sys_idempotency_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE sys_event_log_default;
DROP TABLE sys_event_log;
DROP TABLE sys_audit_default;
DROP TABLE sys_audit;
DROP TABLE sys_idempotency;
DROP TABLE sys_outbox_dlq;
DROP TABLE sys_outbox_default;
DROP TABLE sys_outbox;
DROP TABLE sys_sequences;
DROP FUNCTION fn_sys_idempotency_guard();
DROP FUNCTION is_valid_uuid(p_text text);
DROP FUNCTION safe_uuid(p_text text);
DROP FUNCTION soft_delete_with_timestamp();
DROP FUNCTION update_txid_column();
DROP FUNCTION update_updated_at_column();
DROP FUNCTION gen_random_uuid_v7();
DROP TYPE audit_action;
DROP TYPE idempotency_status;
DROP TYPE outbox_status;
DROP EXTENSION btree_gist;
DROP EXTENSION btree_gin;
DROP EXTENSION pg_trgm;
DROP EXTENSION pgcrypto;
-- +goose StatementEnd
