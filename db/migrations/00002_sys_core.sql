-- +goose Up
-- Description: System infrastructure tables (sequences, outbox, idempotency, audit, sessions,
-- custom field schemas, feature flags, event log, user preferences).

SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- ── sys_sequences ──────────────────────────────────────────────────────────
CREATE TABLE sys_sequences (
    key         VARCHAR(100) NOT NULL PRIMARY KEY,
    current_val BIGINT       NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TRIGGER trg_sys_sequences_updated_at
    BEFORE UPDATE ON sys_sequences
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE sys_sequences IS 'Auto-numbering sequences for documents (INV-2024-00001)';
COMMENT ON COLUMN sys_sequences.key IS 'Sequence key: {prefix}_{period}, e.g., INVOICE_2024';

-- ── sys_outbox (transactional outbox pattern) ──────────────────────────────
CREATE TYPE outbox_status AS ENUM ('pending', 'processing', 'published', 'failed');

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
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE sys_outbox_default PARTITION OF sys_outbox DEFAULT;

CREATE INDEX idx_outbox_pending ON sys_outbox (created_at) WHERE status = 'pending';
CREATE INDEX idx_outbox_retry   ON sys_outbox (next_retry_at) WHERE status = 'pending' AND next_retry_at IS NOT NULL;
CREATE INDEX idx_outbox_stuck   ON sys_outbox (lease_until) WHERE status = 'processing';

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
    failure_reason TEXT
);

-- ── sys_idempotency ────────────────────────────────────────────────────────
CREATE TYPE idempotency_status AS ENUM ('pending', 'success', 'failed');

CREATE TABLE sys_idempotency (
    scope_type           TEXT               NOT NULL
                         CHECK (scope_type IN ('user','merchant','api_key','system')),
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

-- Scope, operation, key and request fingerprint are durable command identity.
-- They must survive result archival as a tombstone and can never be rewritten.
-- +goose StatementBegin
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
-- +goose StatementEnd

CREATE TRIGGER trg_sys_idempotency_guard
    BEFORE UPDATE OR DELETE ON sys_idempotency
    FOR EACH ROW EXECUTE FUNCTION fn_sys_idempotency_guard();

COMMENT ON TABLE sys_idempotency IS 'Durable application command results. Financial command identities are never deleted.';

-- ── sys_audit (partitioned) ────────────────────────────────────────────────
CREATE TYPE audit_action AS ENUM ('create', 'update', 'delete', 'post', 'unpost');

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
) PARTITION BY RANGE (created_at);

CREATE TABLE sys_audit_default PARTITION OF sys_audit DEFAULT;

CREATE INDEX idx_audit_entity ON sys_audit (entity_type, entity_id, created_at DESC);
CREATE INDEX idx_audit_user   ON sys_audit (user_id, created_at DESC);

COMMENT ON TABLE sys_audit IS 'Immutable audit log of all data changes';

-- ── sys_custom_field_schemas ───────────────────────────────────────────────
CREATE TYPE custom_field_type AS ENUM (
    'string', 'text', 'integer', 'decimal', 'boolean',
    'date', 'datetime', 'reference', 'enum', 'json'
);

CREATE TABLE sys_custom_field_schemas (
    id               UUID             PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    entity_type      VARCHAR(50)      NOT NULL,
    field_name       VARCHAR(50)      NOT NULL,
    field_type       custom_field_type NOT NULL,
    display_name     VARCHAR(100)     NOT NULL,
    description      TEXT,
    is_required      BOOLEAN          NOT NULL DEFAULT FALSE,
    is_indexed       BOOLEAN          NOT NULL DEFAULT FALSE,
    default_value    JSONB,
    validation_rules JSONB,
    reference_type   VARCHAR(50),
    enum_values      TEXT[],
    sort_order       INT              NOT NULL DEFAULT 0,
    is_active        BOOLEAN          NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_custom_field UNIQUE (entity_type, field_name)
);

CREATE INDEX idx_custom_fields_entity ON sys_custom_field_schemas (entity_type) WHERE is_active = TRUE;

CREATE TRIGGER trg_sys_custom_fields_updated_at
    BEFORE UPDATE ON sys_custom_field_schemas
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_schema_change()
RETURNS TRIGGER AS $func$
BEGIN
    PERFORM pg_notify('schema_changed', COALESCE(NEW.entity_type, OLD.entity_type));
    RETURN COALESCE(NEW, OLD);
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_custom_fields_notify
    AFTER INSERT OR UPDATE OR DELETE ON sys_custom_field_schemas
    FOR EACH ROW EXECUTE FUNCTION notify_schema_change();

COMMENT ON TABLE sys_custom_field_schemas IS 'Определения пользовательских полей для JSONB attributes';

-- ── sys_feature_flags ──────────────────────────────────────────────────────
CREATE TABLE sys_feature_flags (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    flag_name   VARCHAR(100) NOT NULL,
    description TEXT,
    is_enabled  BOOLEAN      NOT NULL DEFAULT FALSE,
    variant     VARCHAR(50),
    config      JSONB,
    valid_from  TIMESTAMPTZ,
    valid_until TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(50)
);

CREATE UNIQUE INDEX idx_feature_flag_unique ON sys_feature_flags (flag_name);

CREATE TRIGGER trg_sys_feature_flags_updated_at
    BEFORE UPDATE ON sys_feature_flags
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_feature_flags_change()
RETURNS TRIGGER AS $func$
BEGIN
    PERFORM pg_notify('feature_flags_changed', COALESCE(NEW.flag_name, OLD.flag_name));
    RETURN COALESCE(NEW, OLD);
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_feature_flags_notify
    AFTER INSERT OR UPDATE OR DELETE ON sys_feature_flags
    FOR EACH ROW EXECUTE FUNCTION notify_feature_flags_change();

INSERT INTO sys_feature_flags (flag_name, description, is_enabled) VALUES
    ('new_posting_algorithm', 'Use new posting algorithm with line_id tracking', FALSE),
    ('async_posting', 'Enable asynchronous document posting', FALSE),
    ('advanced_reports', 'Enable advanced reporting features', FALSE),
    ('beta_ui', 'Enable beta UI features', FALSE)
ON CONFLICT DO NOTHING;

-- ── sys_event_log (partitioned) ────────────────────────────────────────────
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
) PARTITION BY RANGE (created_at);

CREATE TABLE sys_event_log_default PARTITION OF sys_event_log DEFAULT;

CREATE INDEX idx_event_log_category ON sys_event_log (category, created_at DESC);
CREATE INDEX idx_event_log_user     ON sys_event_log (user_id, created_at DESC);
CREATE INDEX idx_event_log_entity   ON sys_event_log (entity_type, entity_id, created_at DESC);
CREATE INDEX idx_event_log_severity ON sys_event_log (severity, created_at DESC) WHERE severity IN ('error', 'critical');
CREATE INDEX idx_event_log_trace    ON sys_event_log (trace_id, created_at ASC) WHERE trace_id IS NOT NULL;
CREATE INDEX idx_event_log_message_trgm ON sys_event_log USING gin (message gin_trgm_ops);
CREATE INDEX idx_event_log_entity_number ON sys_event_log (entity_number, created_at DESC) WHERE entity_number IS NOT NULL;

COMMENT ON TABLE sys_event_log IS 'System event log — unified journal (analogue of 1C ЖурналРегистрации)';


-- +goose Down
DROP TABLE IF EXISTS sys_event_log;
DROP TABLE IF EXISTS sys_feature_flags;
DROP FUNCTION IF EXISTS notify_feature_flags_change();
DROP TABLE IF EXISTS sys_custom_field_schemas;
DROP FUNCTION IF EXISTS notify_schema_change();
DROP TYPE IF EXISTS custom_field_type;
DROP TABLE IF EXISTS sys_audit;
DROP TYPE IF EXISTS audit_action;
DROP TABLE IF EXISTS sys_idempotency;
DROP FUNCTION IF EXISTS fn_sys_idempotency_guard();
DROP TYPE IF EXISTS idempotency_status;
DROP TABLE IF EXISTS sys_outbox_dlq;
DROP TABLE IF EXISTS sys_outbox;
DROP TYPE IF EXISTS outbox_status;
DROP TABLE IF EXISTS sys_sequences;
