-- +goose Up
-- Description: System infrastructure tables (sequences, outbox, idempotency, audit, sessions,
-- custom field schemas, feature flags, event log, user preferences).

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

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
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE sys_outbox_default PARTITION OF sys_outbox DEFAULT;

CREATE INDEX idx_outbox_pending ON sys_outbox (created_at) WHERE status = 'pending';
CREATE INDEX idx_outbox_retry   ON sys_outbox (next_retry_at) WHERE status = 'pending' AND next_retry_at IS NOT NULL;
CREATE INDEX idx_outbox_stuck   ON sys_outbox (created_at) WHERE status = 'processing';

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
    idempotency_key      VARCHAR(100)      PRIMARY KEY,
    user_id              VARCHAR(50)       NOT NULL,
    operation            VARCHAR(100)      NOT NULL,
    status               idempotency_status NOT NULL DEFAULT 'pending',
    request_hash         VARCHAR(64),
    response             JSONB,
    response_status      INT,
    response_content_type TEXT,
    created_at           TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
    expires_at           TIMESTAMPTZ       NOT NULL
);

CREATE INDEX idx_idempotency_expires ON sys_idempotency (expires_at);

CREATE TRIGGER trg_sys_idempotency_updated_at
    BEFORE UPDATE ON sys_idempotency
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE sys_idempotency IS 'Idempotency keys to prevent duplicate operations';

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

-- ── sys_sessions ───────────────────────────────────────────────────────────
CREATE TABLE sys_sessions (
    refresh_token UUID        PRIMARY KEY,
    user_id       VARCHAR(50) NOT NULL,
    user_email    VARCHAR(255) NOT NULL,
    user_agent    TEXT,
    ip_address    INET,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NOT NULL,
    last_used_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_revoked    BOOLEAN     NOT NULL DEFAULT FALSE,
    revoked_at    TIMESTAMPTZ,
    revoke_reason VARCHAR(50)
);

CREATE INDEX idx_sessions_user    ON sys_sessions (user_id, created_at DESC) WHERE is_revoked = FALSE;
CREATE INDEX idx_sessions_expires ON sys_sessions (expires_at) WHERE is_revoked = FALSE;

COMMENT ON TABLE sys_sessions IS 'Active user sessions with refresh tokens';

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

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS sys_event_log;
DROP TABLE IF EXISTS sys_feature_flags;
DROP FUNCTION IF EXISTS notify_feature_flags_change();
DROP TABLE IF EXISTS sys_custom_field_schemas;
DROP FUNCTION IF EXISTS notify_schema_change();
DROP TYPE IF EXISTS custom_field_type;
DROP TABLE IF EXISTS sys_sessions;
DROP TABLE IF EXISTS sys_audit;
DROP TYPE IF EXISTS audit_action;
DROP TABLE IF EXISTS sys_idempotency;
DROP TYPE IF EXISTS idempotency_status;
DROP TABLE IF EXISTS sys_outbox_dlq;
DROP TABLE IF EXISTS sys_outbox;
DROP TYPE IF EXISTS outbox_status;
DROP TABLE IF EXISTS sys_sequences;
