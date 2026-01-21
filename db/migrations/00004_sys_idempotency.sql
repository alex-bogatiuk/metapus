-- +goose Up
-- Description: Idempotency keys for duplicate request protection
-- Prevents double-posting of documents on network failures

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TYPE idempotency_status AS ENUM ('pending', 'success', 'failed');

CREATE TABLE sys_idempotency (
    idempotency_key VARCHAR(100) PRIMARY KEY,
    user_id VARCHAR(50) NOT NULL,
    operation VARCHAR(100) NOT NULL,       -- Operation name: PostInvoice, CreateOrder
    status idempotency_status NOT NULL DEFAULT 'pending',
    request_hash VARCHAR(64),               -- SHA256 of request body
    response JSONB,                         -- Cached response for replay
    response_status INT,                    -- Cached HTTP status code for replay
    response_content_type TEXT,             -- Cached Content-Type for replay
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

-- Index for cleanup job
CREATE INDEX idx_idempotency_expires ON sys_idempotency (expires_at);

-- Index for tenant isolation


-- Auto-update updated_at
CREATE TRIGGER trg_sys_idempotency_updated_at
    BEFORE UPDATE ON sys_idempotency
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE sys_idempotency IS 'Idempotency keys to prevent duplicate operations';
COMMENT ON COLUMN sys_idempotency.request_hash IS 'SHA256 hash to detect changed requests';
COMMENT ON COLUMN sys_idempotency.response_status IS 'HTTP status code to replay the original response semantics';
COMMENT ON COLUMN sys_idempotency.response_content_type IS 'HTTP Content-Type to replay the original response semantics';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS sys_idempotency;
DROP TYPE IF EXISTS idempotency_status;
