-- +goose Up
-- Description: Worker job execution log for background task observability.
-- Retention: 7 days (cleaned up by worker's hourly cleanup task).

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── sys_worker_jobs ────────────────────────────────────────────────────────
CREATE TYPE worker_job_status AS ENUM ('running', 'success', 'error', 'skipped');

CREATE TABLE sys_worker_jobs (
    id              UUID               PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    job_name        VARCHAR(100)       NOT NULL,
    job_category    VARCHAR(50)        NOT NULL,
    status          worker_job_status  NOT NULL DEFAULT 'running',
    started_at      TIMESTAMPTZ        NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ,
    duration_ms     INTEGER,
    items_processed INTEGER,
    error_message   TEXT,
    metadata        JSONB,
    _txid           BIGINT             DEFAULT txid_current()
);

-- Fast lookups: latest runs per job, and all errors
CREATE INDEX idx_worker_jobs_name_started   ON sys_worker_jobs (job_name, started_at DESC);
CREATE INDEX idx_worker_jobs_started        ON sys_worker_jobs (started_at DESC);
CREATE INDEX idx_worker_jobs_errors         ON sys_worker_jobs (started_at DESC) WHERE status = 'error';

COMMENT ON TABLE sys_worker_jobs IS 'Background worker task execution log (7-day retention)';
COMMENT ON COLUMN sys_worker_jobs.job_name     IS 'Stable identifier, e.g. crypto.expiration, cleanup.sessions';
COMMENT ON COLUMN sys_worker_jobs.job_category IS 'Grouping: crypto | outbox | cleanup | automation';
COMMENT ON COLUMN sys_worker_jobs.items_processed IS 'Rows/records affected (invoices expired, messages relayed, etc.)';
COMMENT ON COLUMN sys_worker_jobs.metadata    IS 'Arbitrary context, e.g. {tenant_id, threshold}';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS sys_worker_jobs;
DROP TYPE  IF EXISTS worker_job_status;
